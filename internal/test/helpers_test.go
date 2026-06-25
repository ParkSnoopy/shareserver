package internaltest

import (
	"context"
	"database/sql"
	stdhttp "net/http"
	"path/filepath"
	"testing"
	"time"

	"shareserver/internal/app"
	"shareserver/internal/config"
	"shareserver/internal/db"
	"shareserver/internal/ent"
	"shareserver/internal/ent/session"
	httpx "shareserver/internal/http"
	"shareserver/internal/share"
	"shareserver/internal/upload"
)

// newClient opens an isolated SQLite database for integration-style tests.
func newClient(t *testing.T) *ent.Client {
	t.Helper()
	client, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// newStore returns a share store plus its backing client for direct assertions.
func newStore(t *testing.T) (*share.Store, *ent.Client) {
	t.Helper()
	client := newClient(t)
	return share.NewStore(client), client
}

// testConfig builds safe temp-path configuration for route and storage tests.
func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		TZ:                time.UTC,
		BlobDir:           t.TempDir(),
		MaxUploadBytes:    10 << 20,
		StorageCapBytes:   1 << 30,
		AppSecret:         []byte("test-secret"),
		TrustProxyHeaders: true,
	}
}

// newTestApp assembles app dependencies without starting an HTTP server.
func newTestApp(t *testing.T) *app.App {
	t.Helper()
	return &app.App{C: testConfig(t), DB: newClient(t)}
}

// newTestHandler exposes handler methods with isolated storage and database state.
func newTestHandler(t *testing.T) (*httpx.Handler, *ent.Client) {
	t.Helper()
	client := newClient(t)
	h := &httpx.Handler{
		A:     &app.App{C: testConfig(t), DB: client},
		Store: share.NewStore(client),
	}
	return h, client
}

// newRouter wires the real router so middleware and routes are exercised together.
func newRouter(t *testing.T) (*app.App, stdhttp.Handler) {
	t.Helper()
	a := newTestApp(t)
	return a, httpx.New(a)
}

// newUploader creates upload policy, store, and blob dir for upload tests.
func newUploader(t *testing.T, cap int64) (*upload.Uploader, *share.Store, string) {
	t.Helper()
	client := newClient(t)
	dir := t.TempDir()
	store := share.NewStore(client)
	u := &upload.Uploader{
		Cfg:   upload.Config{BlobDir: dir, MaxUploadBytes: 10 << 20, StorageCapBytes: cap, AppSecret: []byte("test-secret")},
		Store: store,
		DB:    client,
	}
	return u, store, dir
}

// sampleShare builds consistent share metadata for store, cleanup, and route tests.
func sampleShare(id, vis string, exp string) share.Share {
	var expN sql.NullString
	if exp != "" {
		expN = sql.NullString{String: exp, Valid: true}
	}
	return share.Share{
		ID: id, Title: "t-" + id, Visibility: vis,
		Encrypted: false, Size: 100, BlobPath: "/tmp/" + id + ".blob",
		BlobSHA256: "sum-" + id, UploaderIP: "1.2.3.4",
		ExpiresAt: expN,
	}
}

// mustInsertShare inserts fixture metadata and fails the test on storage errors.
func mustInsertShare(t *testing.T, s *share.Store, sh share.Share) {
	t.Helper()
	if err := s.Insert(sh); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

// insertRouteShare seeds a public share reachable by handler route tests.
func insertRouteShare(t *testing.T, h *httpx.Handler, id, blobPath, expiry string) {
	t.Helper()
	sh := sampleShare(id, "public", expiry)
	sh.BlobPath = blobPath
	if err := h.Store.Insert(sh); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

// existsSession checks whether a session row survived a request path.
func existsSession(t *testing.T, client *ent.Client, id string) bool {
	t.Helper()
	exists, err := client.Session.Query().Where(session.ID(id)).Exist(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

// insertAdminSession seeds an authenticated admin cookie/CSRF pair for route tests.
func insertAdminSession(t *testing.T, client *ent.Client, sid, csrf string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := client.Session.Create().
		SetID(sid).
		SetAdminID(1).
		SetCsrf(csrf).
		SetCreatedAt(now.Format(time.RFC3339Nano)).
		SetExpiresAt(now.Add(time.Hour).Format(time.RFC3339Nano)).
		Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}
