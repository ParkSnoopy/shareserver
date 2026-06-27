package httpx

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"time"

	"shareserver/internal/auth"
)

type ctxKey string

const sessionKey ctxKey = "session"
const clockKey ctxKey = "clock"

// withClock stamps one canonical UTC timestamp into the request context so
// every handler in a single request classifies shares with the same "now".
// Without this, sharePage and blob each call time.Now() independently and a
// share whose expiry falls between the two readings is classified two ways.
// Background goroutines (PurgeExpired, CleanExpiredSessions) keep their own
// local now since they have no request context.
func (h *Handler) withClock(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), clockKey, time.Now().UTC())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requestTime returns the canonical timestamp stamped by withClock, falling
// back to time.Now for paths not wrapped by the middleware.
func requestTime(r *http.Request) time.Time {
	if t, ok := r.Context().Value(clockKey).(time.Time); ok {
		return t
	}
	return time.Now().UTC()
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

// getOrCreateSession asks the lifecycle module for row state and issues cookies in HTTP.
func (h *Handler) getOrCreateSession(w http.ResponseWriter, r *http.Request) Session {
	sid := ""
	if c, err := r.Cookie("sid"); err == nil {
		sid = c.Value
	}
	s, created := h.sessions().GetOrCreate(r.Context(), sid)
	if created {
		h.setCookie(w, s.ID, h.isHTTPS(r))
	}
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
		MaxAge: int(sessionDuration.Seconds()),
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
		if tok == "" && !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/") {
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
	s := h.sessions().Rotate(r.Context(), oldSID, adminID)
	h.setCookie(w, s.ID, h.isHTTPS(r))
}

// logoutSession deletes one session row so the cookie can no longer authorize requests.
func (h *Handler) logoutSession(sid string) {
	h.sessions().Delete(context.Background(), sid)
}

// sessions returns the session lifecycle, defaulting to a DB-backed one.
func (h *Handler) sessions() SessionLifecycle {
	if h.Sessions != nil {
		return h.Sessions
	}
	return NewSessions(h.A.DB)
}
