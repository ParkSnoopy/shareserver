package httpx

import (
	"database/sql"
	"net/http/httptest"
	"testing"
	"time"

	"shareserver/internal/app"
	"shareserver/internal/config"
	"shareserver/internal/db"
	"shareserver/internal/share"
)

// newTestHandler opens an in-file sqlite DB, runs migrations, and returns a
// Handler wired with a Share store. Caller closes via the returned cleanup.
func newTestHandler(t *testing.T) (*Handler, func()) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	cfg := config.Config{TZ: time.UTC}
	h := &Handler{
		A:     &app.App{C: cfg, DB: d},
		Store: share.NewStore(d),
	}
	return h, func() { _ = d.Close() }
}

func TestCleanExpiredSessionsRemovesOnlyExpired(t *testing.T) {
	h, cleanup := newTestHandler(t)
	defer cleanup()
	d := h.A.DB

	now := time.Now().UTC()
	past := now.Add(-time.Hour).Format(time.RFC3339Nano)
	future := now.Add(time.Hour).Format(time.RFC3339Nano)

	// expired row
	if _, err := d.Exec(`insert into sessions(id,admin_id,csrf,created_at,expires_at) values(?,?,?,?,?)`,
		"expired-sid", nil, "c1", past, past); err != nil {
		t.Fatal(err)
	}
	// live row
	if _, err := d.Exec(`insert into sessions(id,admin_id,csrf,created_at,expires_at) values(?,?,?,?,?)`,
		"live-sid", nil, "c2", past, future); err != nil {
		t.Fatal(err)
	}

	n := h.CleanExpiredSessions()
	if n != 1 {
		t.Fatalf("expected 1 expired session removed, got %d", n)
	}

	var count int
	if err := d.QueryRow(`select count(*) from sessions where id='expired-sid'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expired session row still present")
	}
	if err := d.QueryRow(`select count(*) from sessions where id='live-sid'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("live session row was removed; expected to survive")
	}
}

func TestCleanExpiredSessionsNoneExpired(t *testing.T) {
	h, cleanup := newTestHandler(t)
	defer cleanup()
	n := h.CleanExpiredSessions()
	if n != 0 {
		t.Fatalf("expected 0 removed when no expired rows, got %d", n)
	}
}

func TestProxyHeadersRequireTrustedPeer(t *testing.T) {
	h, cleanup := newTestHandler(t)
	defer cleanup()
	h.A.C.TrustProxyHeaders = true

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.10:5000"
	req.Header.Set("X-Forwarded-For", "198.51.100.20")
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := h.clientIP(req); got != "203.0.113.10" {
		t.Fatalf("untrusted peer spoofed client IP: got %q", got)
	}
	if h.isHTTPS(req) {
		t.Fatalf("untrusted peer spoofed https via X-Forwarded-Proto")
	}

	req.RemoteAddr = "127.0.0.1:5000"
	if got := h.clientIP(req); got != "198.51.100.20" {
		t.Fatalf("trusted loopback proxy header ignored: got %q", got)
	}
	if !h.isHTTPS(req) {
		t.Fatalf("trusted loopback proxy https header ignored")
	}
}

// _ keeps the sql import alive if future helpers drop direct use.
var _ = sql.ErrNoRows
