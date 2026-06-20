package httpx

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"shareserver/internal/share"
)

// withChiID returns a request whose chi.URLParam("id") returns id, so handler
// methods can be invoked directly without mounting the full router.
func withChiID(r *http.Request, id string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func insertShare(t *testing.T, h *Handler, id, blobPath, expiry string) {
	t.Helper()
	var exp sql.NullString
	if expiry != "" {
		exp = sql.NullString{String: expiry, Valid: true}
	}
	if err := h.Store.Insert(share.Share{
		ID: id, Title: "t-" + id, Visibility: "public",
		Size: 3, BlobPath: blobPath, BlobSHA256: "sum-" + id, UploaderIP: "1.2.3.4",
		ExpiresAt: exp,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestBlobExpiredReturns410(t *testing.T) {
	h, cleanup := newTestHandler(t)
	defer cleanup()
	// expired 1h ago — no blob file needed, handler returns 410 before ServeFile
	insertShare(t, h, "00000000-0000-0000-0000-000000000001", "/tmp/does-not-exist.blob", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano))

	req := withChiID(httptest.NewRequest(http.MethodGet, "/blob/exp1", nil), "00000000-0000-0000-0000-000000000001")
	w := httptest.NewRecorder()
	h.blob(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410 for expired blob, got %d (body=%q)", w.Code, w.Body.String())
	}
	if w.Body.String() != "expired\n" {
		t.Fatalf("expected body 'expired', got %q", w.Body.String())
	}
}

func TestBlobActiveReturns200(t *testing.T) {
	h, cleanup := newTestHandler(t)
	defer cleanup()
	// real blob file so http.ServeFile succeeds
	dir := t.TempDir()
	bp := filepath.Join(dir, "act1.blob")
	if err := os.WriteFile(bp, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	insertShare(t, h, "00000000-0000-0000-0000-000000000002", bp, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))

	req := withChiID(httptest.NewRequest(http.MethodGet, "/blob/act1", nil), "00000000-0000-0000-0000-000000000002")
	w := httptest.NewRecorder()
	h.blob(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for active blob, got %d (body=%q)", w.Code, w.Body.String())
	}
	if w.Body.String() != "hi" {
		t.Fatalf("expected blob contents 'hi', got %q", w.Body.String())
	}
}

func TestBlobNoExpiryReturns200(t *testing.T) {
	h, cleanup := newTestHandler(t)
	defer cleanup()
	dir := t.TempDir()
	bp := filepath.Join(dir, "noexp.blob")
	if err := os.WriteFile(bp, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	insertShare(t, h, "00000000-0000-0000-0000-000000000003", bp, "") // no expiry -> never expires

	req := withChiID(httptest.NewRequest(http.MethodGet, "/blob/noexp", nil), "00000000-0000-0000-0000-000000000003")
	w := httptest.NewRecorder()
	h.blob(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for no-expiry blob, got %d", w.Code)
	}
}

func TestBlobMissingReturns404(t *testing.T) {
	h, cleanup := newTestHandler(t)
	defer cleanup()
	req := withChiID(httptest.NewRequest(http.MethodGet, "/blob/nope", nil), "nope")
	w := httptest.NewRecorder()
	h.blob(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing share, got %d", w.Code)
	}
}
