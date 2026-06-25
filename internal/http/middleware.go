package httpx

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"shareserver/internal/app"
	"shareserver/internal/auth"
	"shareserver/internal/db"
	"shareserver/internal/share"
	"shareserver/internal/upload"
	"strings"
	"time"
)

type ctxKey string

const sessionKey ctxKey = "session"

// Session is the request-scoped browser session used for CSRF and admin state.
type Session struct {
	ID        string
	AdminID   int64
	CSRF      string
	ExpiresAt time.Time
}

// randHex returns cryptographic random hex for session IDs and CSRF tokens.
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// security applies conservative browser security headers to every response.
func (h *Handler) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; worker-src 'self' blob:; style-src 'self'; font-src 'self'; img-src 'self' blob: data:; media-src 'self' blob:; frame-src 'self' blob:; object-src 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// withSession attaches a valid session to the request, creating one when needed.
func (h *Handler) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := h.getOrCreateSession(w, r)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, s)))
	})
}

// CurrentSession returns the session stored in context or an empty fail-closed value.
func CurrentSession(r *http.Request) Session {
	if s, ok := r.Context().Value(sessionKey).(Session); ok {
		return s
	}
	return Session{}
}

// getOrCreateSession reuses valid sessions and drops expired rows for that visitor.
func (h *Handler) getOrCreateSession(w http.ResponseWriter, r *http.Request) Session {
	if c, err := r.Cookie("sid"); err == nil && c.Value != "" {
		row, err := h.A.DB.Session.Get(r.Context(), c.Value)
		if err == nil {
			s := Session{ID: row.ID, CSRF: row.Csrf}
			if row.AdminID != nil {
				s.AdminID = *row.AdminID
			}
			if t, e := time.Parse(time.RFC3339Nano, row.ExpiresAt); e == nil && t.After(time.Now().UTC()) {
				s.ExpiresAt = t
				return s
			}
			// expired row for this visitor — drop it so it doesn't accumulate.
			_ = h.A.DB.Session.DeleteOneID(c.Value).Exec(r.Context())
		}
	}
	s := Session{ID: randHex(32), CSRF: randHex(32), ExpiresAt: time.Now().Add(12 * time.Hour).UTC()}
	_, _ = h.A.DB.Session.Create().
		SetID(s.ID).
		SetCsrf(s.CSRF).
		SetCreatedAt(db.Now()).
		SetExpiresAt(s.ExpiresAt.Format(time.RFC3339Nano)).
		Save(r.Context())
	h.setCookie(w, s.ID, h.isHTTPS(r))
	return s
}

// isHTTPS reports whether the client is reaching us over TLS, either
// directly or via a trusted proxy that sets X-Forwarded-Proto.
func (h *Handler) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if h.trustProxyHeaders(r) && r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return false
}

// setCookie issues the HTTP-only session cookie with Secure only on trusted HTTPS.
func (h *Handler) setCookie(w http.ResponseWriter, sid string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: "sid", Value: sid, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: secure,
		MaxAge: int((12 * time.Hour).Seconds()),
	})
}

// requestBodyLimit caps all request bodies while allowing configured upload payloads.
func (h *Handler) requestBodyLimit(r *http.Request) int64 {
	if r.URL.Path == "/upload" {
		n := h.A.C.MaxUploadBytes
		if n < 0 {
			n = 0
		}
		return n + (4 << 20)
	}
	return 1 << 20
}

// sameToken compares CSRF tokens without timing leaks.
func sameToken(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// forwardedIP extracts the first valid IP from a proxy forwarding header.
func forwardedIP(v string) string {
	ip := strings.TrimSpace(strings.Split(v, ",")[0])
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

// trustProxyHeaders accepts proxy headers only from configured loopback peers.
func (h *Handler) trustProxyHeaders(r *http.Request) bool {
	if !h.A.C.TrustProxyHeaders {
		return false
	}
	ip := net.ParseIP(auth.CleanIP(r.RemoteAddr))
	return ip != nil && ip.IsLoopback()
}

// csrf rejects unsafe requests unless they present the current session token.
func (h *Handler) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, h.requestBodyLimit(r))
		s := CurrentSession(r)
		tok := r.Header.Get("X-CSRF-Token")
		if tok == "" && r.URL.Path == "/upload" && strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/") {
			http.Error(w, "csrf header required", http.StatusForbidden)
			return
		}
		if tok == "" {
			tok = r.FormValue("csrf")
		}
		if s.CSRF == "" || tok == "" || !sameToken(tok, s.CSRF) {
			http.Error(w, "csrf rejected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin gates admin routes behind a current admin session.
func (h *Handler) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentSession(r).AdminID <= 0 {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the trusted client address used for audit and login bans.
func (h *Handler) clientIP(r *http.Request) string {
	if h.trustProxyHeaders(r) {
		if x := forwardedIP(r.Header.Get("X-Forwarded-For")); x != "" {
			return x
		}
		if x := forwardedIP(r.Header.Get("X-Real-IP")); x != "" {
			return x
		}
	}
	return auth.CleanIP(r.RemoteAddr)
}

// loginSession rotates the session id on successful admin login to defeat
// session fixation: the pre-login sid (which an attacker may have seeded) is
// discarded and a fresh id+csrf is issued bound to the admin.
func (h *Handler) loginSession(w http.ResponseWriter, r *http.Request, oldSID string, adminID int64) {
	newSID := randHex(32)
	newCSRF := randHex(32)
	exp := time.Now().Add(12 * time.Hour).UTC()
	_ = h.A.DB.Session.DeleteOneID(oldSID).Exec(r.Context())
	_, _ = h.A.DB.Session.Create().
		SetID(newSID).
		SetAdminID(adminID).
		SetCsrf(newCSRF).
		SetCreatedAt(db.Now()).
		SetExpiresAt(exp.Format(time.RFC3339Nano)).
		Save(r.Context())
	h.setCookie(w, newSID, h.isHTTPS(r))
}

// logoutSession deletes one session row so the cookie can no longer authorize requests.
func (h *Handler) logoutSession(sid string) {
	_ = h.A.DB.Session.DeleteOneID(sid).Exec(context.Background())
}

// Handler groups HTTP dependencies; route methods keep policy in owned modules.
type Handler struct {
	A      *app.App
	Store  *share.Store
	Upload *upload.Uploader
}
