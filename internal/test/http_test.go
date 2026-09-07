package internaltest

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shareserver/internal/ent/session"
	httpx "shareserver/internal/http"
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

func TestSessionLifecycleRotateDeletesOldAndCreatesAdmin(t *testing.T) {
	_, client := newTestHandler(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_, err := client.Session.Create().
		SetID("pre-login-sid").
		SetCsrf("pre-login-csrf").
		SetLanguage("ko").
		SetCreatedAt(now.Add(-time.Minute).Format(time.RFC3339Nano)).
		SetExpiresAt(now.Add(time.Hour).Format(time.RFC3339Nano)).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sessions := httpx.NewSessions(client)
	sessions.Now = func() time.Time { return now }

	rotated := sessions.Rotate(ctx, "pre-login-sid", 42)
	if rotated.ID == "" || rotated.ID == "pre-login-sid" {
		t.Fatalf("expected fresh session id, got %q", rotated.ID)
	}
	if rotated.AdminID != 42 {
		t.Fatalf("expected admin id 42 on rotated session, got %d", rotated.AdminID)
	}
	if rotated.CSRF == "" {
		t.Fatalf("expected rotated session csrf")
	}
	if rotated.Language != "ko" {
		t.Fatalf("rotated session language = %q, want ko", rotated.Language)
	}
	if existsSession(t, client, "pre-login-sid") {
		t.Fatalf("old session survived rotation")
	}
	row, err := client.Session.Get(ctx, rotated.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.AdminID == nil || *row.AdminID != 42 {
		t.Fatalf("rotated row admin id = %v, want 42", row.AdminID)
	}
	if row.Csrf != rotated.CSRF {
		t.Fatalf("rotated row csrf = %q, want %q", row.Csrf, rotated.CSRF)
	}
}

func TestSessionLifecycleGetOrCreateReusesValidAndDropsExpired(t *testing.T) {
	_, client := newTestHandler(t)
	ctx := context.Background()
	now := time.Now().UTC()
	past := now.Add(-time.Hour).Format(time.RFC3339Nano)
	future := now.Add(time.Hour).Format(time.RFC3339Nano)
	_, err := client.Session.Create().
		SetID("valid-sid").
		SetCsrf("valid-csrf").
		SetCreatedAt(past).
		SetExpiresAt(future).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Session.Create().
		SetID("expired-browser-sid").
		SetCsrf("expired-csrf").
		SetCreatedAt(past).
		SetExpiresAt(now.Add(-time.Minute).Format(time.RFC3339Nano)).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sessions := httpx.NewSessions(client)
	sessions.Now = func() time.Time { return now }

	got, created := sessions.GetOrCreate(ctx, "valid-sid", "ko")
	if created {
		t.Fatalf("valid session was recreated")
	}
	if got.ID != "valid-sid" || got.CSRF != "valid-csrf" {
		t.Fatalf("valid session not reused: %+v", got)
	}
	if !existsSession(t, client, "valid-sid") {
		t.Fatalf("valid session row was removed")
	}

	got, created = sessions.GetOrCreate(ctx, "expired-browser-sid", "ko")
	if !created {
		t.Fatalf("expired session was not replaced")
	}
	if got.ID == "" || got.ID == "expired-browser-sid" || got.CSRF == "" {
		t.Fatalf("replacement session invalid: %+v", got)
	}
	if existsSession(t, client, "expired-browser-sid") {
		t.Fatalf("expired session row survived get/create")
	}
	if !existsSession(t, client, got.ID) {
		t.Fatalf("replacement session row missing")
	}
}

func TestSessionCookieAlwaysRequiresHTTPSWithoutHSTS(t *testing.T) {
	_, router := newRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/upload", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if !strings.Contains(w.Header().Get("Set-Cookie"), "; Secure") {
		t.Fatalf("session cookie missing Secure: %q", w.Header().Get("Set-Cookie"))
	}
	if w.Header().Get("Strict-Transport-Security") != "" {
		t.Fatalf("application must not configure HSTS: %q", w.Header().Get("Strict-Transport-Security"))
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

func TestArchivePageReconcilesDatabaseAndBlobFilesBeforeRendering(t *testing.T) {
	a, router := newRouter(t)
	store := share.NewStore(a.DB)
	orphan := filepath.Join(a.C.BlobDir, "public-page-orphan.blob")
	if err := os.WriteFile(orphan, []byte("orphan"), 0644); err != nil {
		t.Fatal(err)
	}
	missing := sampleShare("00000000-0000-0000-0000-000000000112", "public", futureTS(time.Hour))
	missing.BlobPath = filepath.Join(a.C.BlobDir, "missing-public.blob")
	mustInsertShare(t, store, missing)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected home 200, got %d", w.Code)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan blob survived archive page render: %v", err)
	}
	if _, ok := store.Get(missing.ID); ok {
		t.Fatal("missing-blob database row survived archive page render")
	}
}

func TestSharePageDropsDatabaseRowWhenBlobIsMissing(t *testing.T) {
	a, router := newRouter(t)
	store := share.NewStore(a.DB)
	id := "00000000-0000-0000-0000-000000000113"
	sh := sampleShare(id, "public", futureTS(time.Hour))
	sh.BlobPath = filepath.Join(a.C.BlobDir, "missing-detail.blob")
	mustInsertShare(t, store, sh)

	req := httptest.NewRequest(http.MethodGet, "/s/"+id, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("missing-blob Share page status = %d, want 404", w.Code)
	}
	if _, ok := store.Get(id); ok {
		t.Fatal("missing-blob Share database row survived detail request")
	}
}

func TestLegacyShareWithoutDownloadVerifierIsHidden(t *testing.T) {
	a, router := newRouter(t)
	store := share.NewStore(a.DB)
	id := "00000000-0000-0000-0000-000000000114"
	path := filepath.Join(a.C.BlobDir, id+".blob")
	if err := os.WriteFile(path, []byte("legacy-payload"), 0644); err != nil {
		t.Fatal(err)
	}
	sh := sampleShare(id, "public", futureTS(time.Hour))
	sh.BlobPath = path
	sh.DownloadPasswordHash = ""
	mustInsertShare(t, store, sh)

	home := httptest.NewRequest(http.MethodGet, "/", nil)
	homeResponse := httptest.NewRecorder()
	router.ServeHTTP(homeResponse, home)
	if strings.Contains(homeResponse.Body.String(), id) {
		t.Fatal("legacy Share without download verifier appeared in public list")
	}

	detail := httptest.NewRequest(http.MethodGet, "/s/"+id, nil)
	detailResponse := httptest.NewRecorder()
	router.ServeHTTP(detailResponse, detail)
	if detailResponse.Code != http.StatusNotFound {
		t.Fatalf("legacy Share detail status = %d, want 404", detailResponse.Code)
	}
}

func TestExpiredSharePageShowsCountdownRedirect(t *testing.T) {
	a, router := newRouter(t)
	store := share.NewStore(a.DB)
	id := "00000000-0000-0000-0000-000000000001"
	sh := sampleShare(id, "public", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano))
	sh.BlobPath = filepath.Join(a.C.BlobDir, id+".blob")
	if err := os.WriteFile(sh.BlobPath, []byte("expired"), 0644); err != nil {
		t.Fatal(err)
	}
	mustInsertShare(t, store, sh)

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

func TestAPIDownloadExpiredReturns410AfterPasswordCheck(t *testing.T) {
	a, router := newRouter(t)
	id := "00000000-0000-0000-0000-000000000001"
	insertProtectedShare(t, a, id, "expired", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano), "correct")

	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/"+id, strings.NewReader("{\"password\":\"correct\"}"))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410 for expired blob, got %d (body=%q)", w.Code, w.Body.String())
	}
	if w.Body.String() != "expired\n" {
		t.Fatalf("expected body 'expired', got %q", w.Body.String())
	}
}

func TestAPIDownloadCorrectPasswordReturnsEncryptedPayloadAfterDelay(t *testing.T) {
	a, router := newRouter(t)
	id := "00000000-0000-0000-0000-000000000002"
	insertProtectedShare(t, a, id, "encrypted-payload", time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano), "correct")

	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/"+id, strings.NewReader("{\"password\":\"correct\"}"))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	started := time.Now()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for authorized payload, got %d (body=%q)", w.Code, w.Body.String())
	}
	if w.Body.String() != "encrypted-payload" {
		t.Fatalf("expected encrypted payload, got %q", w.Body.String())
	}
	if elapsed := time.Since(started); elapsed < time.Second {
		t.Fatalf("download delay = %v, want at least 1s", elapsed)
	}
}

func TestAPIDownloadWrongPasswordLeaksNoPayload(t *testing.T) {
	a, router := newRouter(t)
	id := "00000000-0000-0000-0000-000000000003"
	insertProtectedShare(t, a, id, "must-not-leak", "", "correct")

	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/"+id, strings.NewReader("{\"password\":\"wrong\"}"))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-password status = %d, want 401", w.Code)
	}
	if strings.Contains(w.Body.String(), "must-not-leak") {
		t.Fatalf("wrong-password response leaked payload: %q", w.Body.String())
	}
}

func TestLegacyBlobGETLeaksNoPayload(t *testing.T) {
	a, router := newRouter(t)
	id := "00000000-0000-0000-0000-000000000004"
	insertProtectedShare(t, a, id, "must-not-leak", futureTS(time.Hour), "correct")
	req := httptest.NewRequest(http.MethodGet, "/blob/"+id, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("legacy payload route status = %d, want 404", w.Code)
	}
	if strings.Contains(w.Body.String(), "must-not-leak") {
		t.Fatalf("legacy GET leaked payload: %q", w.Body.String())
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

func TestDebugConfigStillRendersProductionPage(t *testing.T) {
	a := newTestApp(t)
	a.C.Dev = true
	router := httpx.New(a)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected dev home 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "# Dev Mode") || strings.Contains(w.Body.String(), "/dev/") {
		t.Fatalf("debug config changed production page:\n%s", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/dev/debug.js", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected removed debug route 404, got %d", w.Code)
	}
}

func TestHTMLPagesHaveNoStoreCacheControl(t *testing.T) {
	_, router := newRouter(t)

	// Home page (200) must not be cached: the Dev flag and CSRF token are
	// per-request and a stale cached HTML can embed a <script> tag for a
	// route that no longer exists (e.g. debug.js after switching to prod),
	// causing the browser to fetch a 404 HTML page as JavaScript/CSS.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("home Cache-Control = %q, want no-store", cc)
	}

	// 404 page must also be no-store so the browser does not cache a stale
	// error page and miss a newly-created share at the same path.
	req = httptest.NewRequest(http.MethodGet, "/s/nope", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("404 Cache-Control = %q, want no-store", cc)
	}
}

func TestStaticFilesHaveRevalidateCacheControl(t *testing.T) {
	_, router := newRouter(t)

	// Static JS/CSS must revalidate so a browser that cached an old
	// progress.js (before a server-side update) always re-checks with
	// If-Modified-Since instead of serving a stale API mismatch. The
	// repoFile-based root resolves web/static from the test's working
	// directory too, so app.css is served as a real 200.
	req := httptest.NewRequest(http.MethodGet, "/static/css/app.css", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected static CSS 200, got %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache, must-revalidate" {
		t.Fatalf("static Cache-Control = %q, want no-cache, must-revalidate", cc)
	}

	req = httptest.NewRequest(http.MethodGet, "/static/img/favicon.ico", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected favicon 200, got %d", w.Code)
	}
}

func TestBaseTemplateIncludesFavicon(t *testing.T) {
	_, router := newRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected home 200, got %d", w.Code)
	}
	assertBodyContains(t, w.Body.String(), `<link rel="icon" href="/static/img/favicon.ico" sizes="any">`)
}

func TestAdminDashboardOmitsManualStorageCleanupAction(t *testing.T) {
	a, router := newRouter(t)
	insertAdminSession(t, a.DB, "admin-sid", "admin-csrf")

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: "sid", Value: "admin-sid"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected admin dashboard 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "admin.cleanStorage") || strings.Contains(w.Body.String(), "/admin/storage/cleanup") {
		t.Fatalf("admin dashboard still exposes manual storage cleanup:\n%s", w.Body.String())
	}
}

func TestAdminSharesReconcilesDatabaseAndBlobFilesBeforeRendering(t *testing.T) {
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
	missing := sampleShare("00000000-0000-0000-0000-000000000107", "public", futureTS(time.Hour))
	missing.BlobPath = filepath.Join(a.C.BlobDir, "missing.blob")
	mustInsertShare(t, store, missing)

	req := httptest.NewRequest(http.MethodGet, "/admin/shares", nil)
	req.AddCookie(&http.Cookie{Name: "sid", Value: "cleanup-admin-sid"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected admin shares 200, got %d", w.Code)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan file survived admin shares render: %v", err)
	}
	if _, err := os.Stat(registered); err != nil {
		t.Fatalf("registered file was removed: %v", err)
	}
	if _, ok := store.Get(missing.ID); ok {
		t.Fatal("database row with missing blob survived admin shares render")
	}
	if _, ok := store.Get(sh.ID); !ok {
		t.Fatal("synchronized Share row was removed")
	}
}

func TestAdminSharesOffersBulkSelection(t *testing.T) {
	a, router := newRouter(t)
	insertAdminSession(t, a.DB, "bulk-ui-admin-sid", "bulk-ui-csrf")
	store := share.NewStore(a.DB)
	id := "00000000-0000-0000-0000-000000000108"
	blob := filepath.Join(a.C.BlobDir, id+".blob")
	if err := os.WriteFile(blob, []byte("select me"), 0644); err != nil {
		t.Fatal(err)
	}
	sh := sampleShare(id, "public", futureTS(time.Hour))
	sh.BlobPath = blob
	mustInsertShare(t, store, sh)

	req := httptest.NewRequest(http.MethodGet, "/admin/shares", nil)
	req.AddCookie(&http.Cookie{Name: "sid", Value: "bulk-ui-admin-sid"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected admin shares 200, got %d", w.Code)
	}
	assertBodyContains(t, w.Body.String(),
		`action="/admin/shares/delete"`,
		`id="selectAllShares"`,
		`name="ids" value="`+id+`"`,
		`data-i18n="admin.deleteSelected"`,
		`/static/js/admin-shares.js`,
	)
}

func TestAdminBulkDeleteRemovesSelectedBlobAndDatabasePairs(t *testing.T) {
	a, router := newRouter(t)
	insertAdminSession(t, a.DB, "bulk-delete-admin-sid", "bulk-delete-csrf")
	store := share.NewStore(a.DB)
	selected := []string{
		"00000000-0000-0000-0000-000000000109",
		"00000000-0000-0000-0000-000000000110",
	}
	unselected := "00000000-0000-0000-0000-000000000111"
	paths := map[string]string{}
	for _, id := range append(selected, unselected) {
		blob := filepath.Join(a.C.BlobDir, id+".blob")
		if err := os.WriteFile(blob, []byte(id), 0644); err != nil {
			t.Fatal(err)
		}
		sh := sampleShare(id, "public", futureTS(time.Hour))
		sh.BlobPath = blob
		mustInsertShare(t, store, sh)
		paths[id] = blob
	}

	body := "csrf=bulk-delete-csrf&ids=" + selected[0] + "&ids=" + selected[1]
	req := httptest.NewRequest(http.MethodPost, "/admin/shares/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "sid", Value: "bulk-delete-admin-sid"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/shares?delete=done&removed=2&failed=0" {
		t.Fatalf("bulk delete response = %d %q", w.Code, w.Header().Get("Location"))
	}
	for _, id := range selected {
		if _, ok := store.Get(id); ok {
			t.Fatalf("selected database row %s remains", id)
		}
		if _, err := os.Stat(paths[id]); !os.IsNotExist(err) {
			t.Fatalf("selected blob %s remains: %v", id, err)
		}
	}
	if _, ok := store.Get(unselected); !ok {
		t.Fatal("unselected database row was removed")
	}
	if _, err := os.Stat(paths[unselected]); err != nil {
		t.Fatalf("unselected blob was removed: %v", err)
	}
}

func TestAdminBulkDeleteDropsMetadataWhenRegisteredPathIsNotAFile(t *testing.T) {
	a, router := newRouter(t)
	insertAdminSession(t, a.DB, "failed-delete-admin-sid", "failed-delete-admin-csrf")
	store := share.NewStore(a.DB)
	id := "00000000-0000-0000-0000-000000000106"
	sh := sampleShare(id, "public", futureTS(time.Hour))
	sh.BlobPath = filepath.Join(a.C.BlobDir, "directory-not-blob")
	if err := os.Mkdir(sh.BlobPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sh.BlobPath, "child"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	mustInsertShare(t, store, sh)

	req := httptest.NewRequest(http.MethodPost, "/admin/shares/delete", strings.NewReader("csrf=failed-delete-admin-csrf&ids="+id))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "sid", Value: "failed-delete-admin-sid"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/shares?delete=done&removed=1&failed=0" {
		t.Fatalf("bulk delete response = %d %q", w.Code, w.Header().Get("Location"))
	}
	if _, ok := store.Get(id); ok {
		t.Fatal("database row survived after registered blob path was not a file")
	}
	if _, err := os.Stat(filepath.Join(sh.BlobPath, "child")); !os.IsNotExist(err) {
		t.Fatalf("orphan child file survived: %v", err)
	}
}
