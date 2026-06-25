package internaltest

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shareserver/internal/upload"
)

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
	u, _, dir := newUploader(t, 1<<30)
	_, err := u.Do(upload.Request{
		Title: "x", Visibility: "private", PrivateKey: "",
		ExpiryHours: "6", Reader: strReader("hello"), UploaderIP: "1.2.3.4",
	})
	if !errors.Is(err, upload.ErrPrivateKeyRequired) {
		t.Fatalf("expected ErrPrivateKeyRequired, got %v", err)
	}
	if n := countBlobs(t, dir); n != 0 {
		t.Fatalf("orphan blob written on validation failure: %d files", n)
	}
}

func TestCapReachedWritesNoBlob(t *testing.T) {
	u, _, dir := newUploader(t, 0)
	_, err := u.Do(upload.Request{
		Title: "x", Visibility: "public", ExpiryHours: "6",
		Reader: strReader("hello"), UploaderIP: "1.2.3.4",
	})
	if !errors.Is(err, upload.ErrCap) {
		t.Fatalf("expected ErrCap, got %v", err)
	}
	if n := countBlobs(t, dir); n != 0 {
		t.Fatalf("blob written despite cap reached: %d files", n)
	}
}

func TestSuccessfulUploadInsertsRowAndBlob(t *testing.T) {
	u, store, dir := newUploader(t, 1<<30)
	res, err := u.Do(upload.Request{
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
	u, store, _ := newUploader(t, 1<<30)
	res, err := u.Do(upload.Request{
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

func TestNonAdminSevenDayExpiryClampedTo24h(t *testing.T) {
	u, store, _ := newUploader(t, 1<<30)
	res, err := u.Do(upload.Request{
		Title: "x", Visibility: "public", ExpiryHours: "168",
		Reader: strReader("hello"), UploaderIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	sh, _ := store.Get(res.ID)
	exp, err := time.Parse(time.RFC3339Nano, sh.ExpiresAt.String)
	if err != nil {
		t.Fatal(err)
	}
	dur := exp.Sub(time.Now().UTC())
	if dur > 24*time.Hour+time.Minute || dur < 23*time.Hour {
		t.Fatalf("non-admin 7d expiry not clamped to ~24h: %v", dur)
	}
}

func TestAdminExpiryAllowsThreeMonths(t *testing.T) {
	u, store, _ := newUploader(t, 1<<30)
	res, err := u.Do(upload.Request{
		Title: "x", Visibility: "public", ExpiryHours: "2160", Admin: true,
		Reader: strReader("hello"), UploaderIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	sh, _ := store.Get(res.ID)
	exp, err := time.Parse(time.RFC3339Nano, sh.ExpiresAt.String)
	if err != nil {
		t.Fatal(err)
	}
	dur := exp.Sub(time.Now().UTC())
	if dur > 90*24*time.Hour+time.Minute || dur < 90*24*time.Hour-time.Minute {
		t.Fatalf("admin expiry not ~90d: %v", dur)
	}
}

func TestExpiryDefault6h(t *testing.T) {
	u, store, _ := newUploader(t, 1<<30)
	res, err := u.Do(upload.Request{
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

func TestMetadataTooLargeRejected(t *testing.T) {
	u, _, dir := newUploader(t, 1<<30)
	big := strings.Repeat("x", (64<<10)+1)
	_, err := u.Do(upload.Request{
		Title: "x", Visibility: "public", ExpiryHours: "6",
		ZipManifest: big,
		Reader:      strReader("hello"), UploaderIP: "1.2.3.4",
	})
	if !errors.Is(err, upload.ErrMetadataTooLarge) {
		t.Fatalf("expected ErrMetadataTooLarge, got %v", err)
	}
	if n := countBlobs(t, dir); n != 0 {
		t.Fatalf("blob written on metadata rejection: %d files", n)
	}
}

func TestEncryptedUploadStripsManifest(t *testing.T) {
	u, store, _ := newUploader(t, 1<<30)
	manifest := `[{"name":"secret.txt","size":10,"type":"text/plain"}]`
	res, err := u.Do(upload.Request{
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
	u, store, _ := newUploader(t, 1<<30)
	manifest := `[{"name":"note.txt","size":5,"type":"text/plain"}]`
	res, err := u.Do(upload.Request{
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
