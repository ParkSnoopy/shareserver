package upload

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shareserver/internal/db"
	"shareserver/internal/share"
)

// newUploader builds an Uploader against an in-memory DB + temp blob dir.
func newUploader(t *testing.T, cap int64) (*Uploader, *share.Store, string, func()) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	dir := t.TempDir()
	store := share.NewStore(d)
	u := &Uploader{
		Cfg:   Config{BlobDir: dir, MaxUploadBytes: 10 << 20, StorageCapBytes: cap, AppSecret: []byte("test-secret")},
		Store: store,
		DB:    d,
	}
	return u, store, dir, func() { _ = d.Close() }
}

func countBlobs(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

func TestPrivateKeyRequiredWritesNoBlob(t *testing.T) {
	u, _, dir, cleanup := newUploader(t, 1<<30)
	defer cleanup()
	_, err := u.Do(Request{
		Title: "x", Visibility: "private", PrivateKey: "",
		ExpiryHours: "6", Reader: strReader("hello"), UploaderIP: "1.2.3.4",
	})
	if !errors.Is(err, ErrPrivateKeyRequired) {
		t.Fatalf("expected ErrPrivateKeyRequired, got %v", err)
	}
	if n := countBlobs(t, dir); n != 0 {
		t.Fatalf("orphan blob written on validation failure: %d files", n)
	}
}

func TestCapReachedWritesNoBlob(t *testing.T) {
	// cap 0 -> always full; a valid (public) request should reject before store.
	u, _, dir, cleanup := newUploader(t, 0)
	defer cleanup()
	_, err := u.Do(Request{
		Title: "x", Visibility: "public", ExpiryHours: "6",
		Reader: strReader("hello"), UploaderIP: "1.2.3.4",
	})
	if !errors.Is(err, ErrCap) {
		t.Fatalf("expected ErrCap, got %v", err)
	}
	if n := countBlobs(t, dir); n != 0 {
		t.Fatalf("blob written despite cap reached: %d files", n)
	}
}

func TestSuccessfulUploadInsertsRowAndBlob(t *testing.T) {
	u, store, dir, cleanup := newUploader(t, 1<<30)
	defer cleanup()
	res, err := u.Do(Request{
		Title: "ok", Visibility: "public", ExpiryHours: "6",
		CipherMeta: "{}", ZipManifest: "[]",
		Reader: strReader("hello shareserver"), UploaderIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if res.ID == "" || res.URL != "/s/"+res.ID {
		t.Fatalf("bad result: %+v", res)
	}
	if n := countBlobs(t, dir); n != 1 {
		t.Fatalf("expected 1 blob, got %d", n)
	}
	sh, ok := store.Get(res.ID)
	if !ok {
		t.Fatalf("share row not inserted")
	}
	if sh.Title != "ok" || sh.Visibility != "public" {
		t.Fatalf("stored share wrong: %+v", sh)
	}
	if _, err := os.Stat(filepath.Clean(sh.BlobPath)); err != nil {
		t.Fatalf("blob path from row missing: %v", err)
	}
}

func TestExpiryClampedTo24h(t *testing.T) {
	u, store, _, cleanup := newUploader(t, 1<<30)
	defer cleanup()
	res, err := u.Do(Request{
		Title: "x", Visibility: "public", ExpiryHours: "9999",
		Reader: strReader("hello"), UploaderIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	sh, _ := store.Get(res.ID)
	if !sh.ExpiresAt.Valid {
		t.Fatal("expiry not set")
	}
	exp, err := time.Parse(time.RFC3339Nano, sh.ExpiresAt.String)
	if err != nil {
		t.Fatal(err)
	}
	dur := exp.Sub(time.Now().UTC())
	if dur > 24*time.Hour+time.Minute || dur < 23*time.Hour {
		t.Fatalf("expiry not clamped to ~24h: %v", dur)
	}
}

func TestExpiryDefault6h(t *testing.T) {
	u, store, _, cleanup := newUploader(t, 1<<30)
	defer cleanup()
	res, err := u.Do(Request{
		Title: "x", Visibility: "public", ExpiryHours: "0",
		Reader: strReader("hello"), UploaderIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	sh, _ := store.Get(res.ID)
	exp, _ := time.Parse(time.RFC3339Nano, sh.ExpiresAt.String)
	dur := exp.Sub(time.Now().UTC())
	if dur > 6*time.Hour+time.Minute || dur < 5*time.Hour {
		t.Fatalf("expiry not defaulted to ~6h: %v", dur)
	}
}

// strReader returns a reader yielding s then io.EOF, so storage.Store's
// io.Copy completes cleanly.
func strReader(s string) *bytesReader { return &bytesReader{b: []byte(s)} }

type bytesReader struct {
	b []byte
	i int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func TestMetadataTooLargeRejected(t *testing.T) {
	u, _, dir, cleanup := newUploader(t, 1<<30)
	defer cleanup()
	big := strings.Repeat("x", (64<<10)+1)
	_, err := u.Do(Request{
		Title: "x", Visibility: "public", ExpiryHours: "6",
		ZipManifest: big,
		Reader:      strReader("hello"), UploaderIP: "1.2.3.4",
	})
	if !errors.Is(err, ErrMetadataTooLarge) {
		t.Fatalf("expected ErrMetadataTooLarge, got %v", err)
	}
	if n := countBlobs(t, dir); n != 0 {
		t.Fatalf("blob written on metadata rejection: %d files", n)
	}
}

func TestEncryptedUploadStripsManifest(t *testing.T) {
	u, store, _, cleanup := newUploader(t, 1<<30)
	defer cleanup()
	manifest := `[{"name":"secret.txt","size":10,"type":"text/plain"}]`
	res, err := u.Do(Request{
		Title: "enc", Visibility: "public", ExpiryHours: "6",
		EncryptedFlag: "1", CipherMeta: `{}`, ZipManifest: manifest,
		Reader: strReader("encrypted-bytes"), UploaderIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	sh, _ := store.Get(res.ID)
	if !sh.Encrypted {
		t.Fatal("share not marked encrypted")
	}
	if sh.ZipManifest != "[]" {
		t.Fatalf("encrypted share leaked manifest: got %q, want \"[]\"", sh.ZipManifest)
	}
}

func TestPlainUploadKeepsManifest(t *testing.T) {
	u, store, _, cleanup := newUploader(t, 1<<30)
	defer cleanup()
	manifest := `[{"name":"note.txt","size":5,"type":"text/plain"}]`
	res, err := u.Do(Request{
		Title: "plain", Visibility: "public", ExpiryHours: "6",
		ZipManifest: manifest,
		Reader:      strReader("hello"), UploaderIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	sh, _ := store.Get(res.ID)
	if sh.ZipManifest != manifest {
		t.Fatalf("plain share manifest not preserved: got %q", sh.ZipManifest)
	}
}
