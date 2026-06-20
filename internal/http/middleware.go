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

type Session struct {
	ID        string
	AdminID   int64
	CSRF      string
	ExpiresAt time.Time
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func (h *Handler) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; worker-src 'self' blob:; style-src 'self'; font-src 'self'; img-src 'self' blob: data:; media-src 'self' blob:; frame-src 'self' blob:; object-src 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := h.getOrCreateSession(w, r)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, s)))
	})
}

func CurrentSession(r *http.Request) Session {
	if s, ok := r.Context().Value(sessionKey).(Session); ok {
		return s
	}
	return Session{}
}

func (h *Handler) getOrCreateSession(w http.ResponseWriter, r *http.Request) Session {
	if c, err := r.Cookie("sid"); err == nil && c.Value != "" {
		var s Session
		var exp string
		err := h.A.DB.QueryRow(`select id, coalesce(admin_id,0), csrf, expires_at from sessions where id=?`, c.Value).Scan(&s.ID, &s.AdminID, &s.CSRF, &exp)
		if err == nil {
			if t, e := time.Parse(time.RFC3339Nano, exp); e == nil && t.After(time.Now().UTC()) {
				s.ExpiresAt = t
				return s
			}
			// expired row for this visitor — drop it so it doesn't accumulate.
			_, _ = h.A.DB.Exec(`delete from sessions where id=?`, c.Value)
		}
	}
	s := Session{ID: randHex(32), CSRF: randHex(32), ExpiresAt: time.Now().Add(12 * time.Hour).UTC()}
	_, _ = h.A.DB.Exec(`insert into sessions(id,admin_id,csrf,created_at,expires_at) values(?,null,?,?,?)`, s.ID, s.CSRF, db.Now(), s.ExpiresAt.Format(time.RFC3339Nano))
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

func (h *Handler) setCookie(w http.ResponseWriter, sid string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: "sid", Value: sid, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: secure,
		MaxAge: int((12 * time.Hour).Seconds()),
	})
}

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

func sameToken(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func forwardedIP(v string) string {
	ip := strings.TrimSpace(strings.Split(v, ",")[0])
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

func (h *Handler) trustProxyHeaders(r *http.Request) bool {
	if !h.A.C.TrustProxyHeaders {
		return false
	}
	ip := net.ParseIP(auth.CleanIP(r.RemoteAddr))
	return ip != nil && ip.IsLoopback()
}

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

func (h *Handler) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentSession(r).AdminID <= 0 {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

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
	exp := time.Now().Add(12 * time.Hour).UTC().Format(time.RFC3339Nano)
	_, _ = h.A.DB.Exec(`delete from sessions where id=?`, oldSID)
	_, _ = h.A.DB.Exec(`insert into sessions(id,admin_id,csrf,created_at,expires_at) values(?,?,?,?,?)`, newSID, adminID, newCSRF, db.Now(), exp)
	h.setCookie(w, newSID, h.isHTTPS(r))
}
func (h *Handler) logoutSession(sid string) {
	_, _ = h.A.DB.Exec(`delete from sessions where id=?`, sid)
}

type Handler struct {
	A      *app.App
	Store  *share.Store
	Upload *upload.Uploader
}
