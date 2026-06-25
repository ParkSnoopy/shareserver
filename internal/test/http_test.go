package internaltest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shareserver/internal/ent/session"
	"shareserver/internal/share"
)

func assertBodyContains(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q in:\n%s", want, body)
		}
	}
}

func TestCleanExpiredSessionsRemovesOnlyExpired(t *testing.T) {
	h, client := newTestHandler(t)
	now := time.Now().UTC()
	past := now.Add(-time.Hour).Format(time.RFC3339Nano)
	future := now.Add(time.Hour).Format(time.RFC3339Nano)

	_, err := client.Session.Create().
		SetID("expired-sid").
		SetCsrf("c1").
		SetCreatedAt(past).
		SetExpiresAt(past).
		Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Session.Create().
		SetID("live-sid").
		SetCsrf("c2").
		SetCreatedAt(past).
		SetExpiresAt(future).
		Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	n := h.CleanExpiredSessions()
	if n != 1 {
		t.Fatalf("expected 1 expired session removed, got %d", n)
	}
	if existsSession(t, client, "expired-sid") {
		t.Fatalf("expired session row still present")
	}
	if !existsSession(t, client, "live-sid") {
		t.Fatalf("live session row was removed; expected to survive")
	}
}

func TestCleanExpiredSessionsNoneExpired(t *testing.T) {
	h, _ := newTestHandler(t)
	n := h.CleanExpiredSessions()
	if n != 0 {
		t.Fatalf("expected 0 removed when no expired rows, got %d", n)
	}
}

func TestProxyHTTPSHeadersRequireTrustedPeer(t *testing.T) {
	_, router := newRouter(t)

	untrusted := httptest.NewRequest(http.MethodGet, "/upload", nil)
	untrusted.RemoteAddr = "203.0.113.10:5000"
	untrusted.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, untrusted)
	if strings.Contains(w.Header().Get("Set-Cookie"), "; Secure") {
		t.Fatalf("untrusted peer spoofed secure cookie: %q", w.Header().Get("Set-Cookie"))
	}

	trusted := httptest.NewRequest(http.MethodGet, "/upload", nil)
	trusted.RemoteAddr = "127.0.0.1:5000"
	trusted.Header.Set("X-Forwarded-Proto", "https")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, trusted)
	if !strings.Contains(w.Header().Get("Set-Cookie"), "; Secure") {
		t.Fatalf("trusted loopback proxy https header ignored: %q", w.Header().Get("Set-Cookie"))
	}
}

func TestNotFoundPageShowsCountdownRedirect(t *testing.T) {
	_, router := newRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/s/nope", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 page status, got %d", w.Code)
	}
	assertBodyContains(t, w.Body.String(),
		"# 404",
		"data-redirect-countdown",
		"data-seconds=\"5\"",
		"data-redirect-to=\"/\"",
		"/static/js/redirect-countdown.js",
	)
}

func TestExpiredSharePageShowsCountdownRedirect(t *testing.T) {
	a, router := newRouter(t)
	store := share.NewStore(a.DB)
	id := "00000000-0000-0000-0000-000000000001"
	mustInsertShare(t, store, sampleShare(id, "public", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)))

	req := httptest.NewRequest(http.MethodGet, "/s/"+id, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected expired page status 200, got %d", w.Code)
	}
	assertBodyContains(t, w.Body.String(),
		"expired.",
		"data-redirect-countdown",
		"data-seconds=\"5\"",
		"data-redirect-to=\"/\"",
		"/static/js/redirect-countdown.js",
	)
}

func TestBlobExpiredReturns410(t *testing.T) {
	a, router := newRouter(t)
	store := share.NewStore(a.DB)
	id := "00000000-0000-0000-0000-000000000001"
	mustInsertShare(t, store, sampleShare(id, "public", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)))

	req := httptest.NewRequest(http.MethodGet, "/blob/"+id, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410 for expired blob, got %d (body=%q)", w.Code, w.Body.String())
	}
	if w.Body.String() != "expired\n" {
		t.Fatalf("expected body 'expired', got %q", w.Body.String())
	}
}

func TestBlobActiveReturns200(t *testing.T) {
	a, router := newRouter(t)
	store := share.NewStore(a.DB)
	id := "00000000-0000-0000-0000-000000000002"
	bp := filepath.Join(t.TempDir(), "act1.blob")
	if err := os.WriteFile(bp, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	sh := sampleShare(id, "public", time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))
	sh.BlobPath = bp
	mustInsertShare(t, store, sh)

	req := httptest.NewRequest(http.MethodGet, "/blob/"+id, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for active blob, got %d (body=%q)", w.Code, w.Body.String())
	}
	if w.Body.String() != "hi" {
		t.Fatalf("expected blob contents 'hi', got %q", w.Body.String())
	}
}

func TestBlobNoExpiryReturns200(t *testing.T) {
	a, router := newRouter(t)
	store := share.NewStore(a.DB)
	id := "00000000-0000-0000-0000-000000000003"
	bp := filepath.Join(t.TempDir(), "noexp.blob")
	if err := os.WriteFile(bp, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	sh := sampleShare(id, "public", "")
	sh.BlobPath = bp
	mustInsertShare(t, store, sh)

	req := httptest.NewRequest(http.MethodGet, "/blob/"+id, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for no-expiry blob, got %d", w.Code)
	}
}

func TestBlobMissingReturns404(t *testing.T) {
	_, router := newRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/blob/nope", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing share, got %d", w.Code)
	}
}

func TestAnonymousSessionCreatedThroughRouter(t *testing.T) {
	a, router := newRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "sid" {
		t.Fatalf("session cookie missing: %+v", cookies)
	}
	exists, err := a.DB.Session.Query().Where(session.ID(cookies[0].Value)).Exist(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatalf("session row missing for cookie")
	}
}

func TestAdminDashboardShowsStorageCleanupAction(t *testing.T) {
	a, router := newRouter(t)
	insertAdminSession(t, a.DB, "admin-sid", "admin-csrf")

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "sid", Value: "admin-sid"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected admin dashboard 200, got %d", w.Code)
	}
	assertBodyContains(t, w.Body.String(),
		"clean up stored files now",
		"action=\"/admin/storage/cleanup\"",
	)
}

func TestAdminStorageCleanupRemovesUnregisteredFiles(t *testing.T) {
	a, router := newRouter(t)
	insertAdminSession(t, a.DB, "cleanup-admin-sid", "cleanup-csrf")
	if err := os.MkdirAll(a.C.BlobDir, 0755); err != nil {
		t.Fatal(err)
	}
	registered := filepath.Join(a.C.BlobDir, "registered.file")
	orphan := filepath.Join(a.C.BlobDir, "unregistered.tmp")
	for _, path := range []string{registered, orphan} {
		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	store := share.NewStore(a.DB)
	sh := sampleShare("00000000-0000-0000-0000-000000000104", "public", futureTS(time.Hour))
	sh.BlobPath = registered
	mustInsertShare(t, store, sh)

	req := httptest.NewRequest(http.MethodPost, "/admin/storage/cleanup", strings.NewReader("csrf=cleanup-csrf"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "sid", Value: "cleanup-admin-sid"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected cleanup redirect, got %d", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/admin?storage_cleanup=done&missing=0&orphan=1" {
		t.Fatalf("unexpected cleanup redirect: %q", got)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("unregistered file still exists: %v", err)
	}
	if _, err := os.Stat(registered); err != nil {
		t.Fatalf("registered file was removed: %v", err)
	}
}
