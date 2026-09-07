package internaltest

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shareserver/internal/share"
	"shareserver/internal/upload"
)

const testCipherMeta = "{\"kdf\":\"PBKDF2-SHA-384\",\"iterations\":600000,\"salt\":\"AAAAAAAAAAAAAAAAAAAAAA==\",\"cipher\":\"AES-256-GCM\",\"nonce\":\"AAAAAAAAAAAAAAAA\"}"

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
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta,
		ExpiryHours: "6", Reader: strReader("encrypted payload"), UploaderIP: "1.2.3.4",
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
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta,
		Reader: strReader("encrypted payload"), UploaderIP: "1.2.3.4",
	})
	if !errors.Is(err, upload.ErrCap) {
		t.Fatalf("expected ErrCap, got %v", err)
	}
	if n := countBlobs(t, dir); n != 0 {
		t.Fatalf("blob written despite cap reached: %d files", n)
	}
}

func TestUploadPurgesExpiredShareBeforeCapCheck(t *testing.T) {
	u, store, dir := newUploader(t, int64(len("encrypted payload")))
	id := "00000000-0000-0000-0000-000000000201"
	blob := filepath.Join(dir, id+".blob")
	if err := os.WriteFile(blob, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	sh := sampleShare(id, "public", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano))
	sh.BlobPath = blob
	mustInsertShare(t, store, sh)

	res, err := u.Do(upload.Request{
		Title: "replacement", Visibility: "public", ExpiryHours: "6",
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta,
		Reader: strReader("encrypted payload"), UploaderIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("upload rejected by expired blob usage: %v", err)
	}
	if _, ok := store.Get(id); ok {
		t.Fatal("expired share row survived upload cap cleanup")
	}
	if _, err := os.Stat(blob); !os.IsNotExist(err) {
		t.Fatalf("expired blob survived upload cap cleanup: %v", err)
	}
	if _, ok := store.Get(res.ID); !ok {
		t.Fatal("replacement share row missing")
	}
	if got := len(store.ListPublic(share.ActiveAt(time.Now().UTC()))); got != 1 {
		t.Fatalf("active share count = %d, want 1", got)
	}
}

func TestStorageReconcileIgnoresInProgressUploadStaging(t *testing.T) {
	u, store, _ := newUploader(t, 10<<20)
	reader := &blockingReader{started: make(chan struct{}), release: make(chan struct{})}
	uploadDone := make(chan error, 1)
	go func() {
		res, err := u.Do(upload.Request{
			Title: "concurrent", Visibility: "public", ExpiryHours: "6",
			DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta,
			Reader: reader, UploaderIP: "1.2.3.4",
		})
		if err == nil {
			if _, ok := store.Get(res.ID); !ok {
				err = errors.New("upload metadata row missing")
			}
		}
		uploadDone <- err
	}()
	<-reader.started

	reconcileDone := make(chan struct{})
	go func() {
		u.Integrity.Reconcile()
		close(reconcileDone)
	}()
	select {
	case <-reconcileDone:
	case <-time.After(time.Second):
		close(reader.release)
		<-uploadDone
		t.Fatal("storage reconciliation blocked on an in-progress upload")
	}
	_, err := u.Do(upload.Request{
		Title: "second", Visibility: "public", ExpiryHours: "6",
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta,
		Reader: strReader("another encrypted payload"), UploaderIP: "1.2.3.5",
	})
	if !errors.Is(err, upload.ErrCap) {
		close(reader.release)
		<-uploadDone
		t.Fatalf("second upload bypassed in-flight capacity reservation: %v", err)
	}

	close(reader.release)
	if err := <-uploadDone; err != nil {
		t.Fatalf("upload failed during concurrent reconciliation: %v", err)
	}
}

func TestSuccessfulUploadInsertsRowAndBlob(t *testing.T) {
	u, store, dir := newUploader(t, 1<<30)
	res, err := u.Do(upload.Request{
		Title: "ok", Visibility: "public", ExpiryHours: "6",
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta, ZipManifest: "[]",
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
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta,
		Reader: strReader("encrypted payload"), UploaderIP: "1.2.3.4",
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
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta,
		Reader: strReader("encrypted payload"), UploaderIP: "1.2.3.4",
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
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta,
		Reader: strReader("encrypted payload"), UploaderIP: "1.2.3.4",
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
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta,
		Reader: strReader("encrypted payload"), UploaderIP: "1.2.3.4",
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
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta,
		ZipManifest: big,
		Reader:      strReader("encrypted payload"), UploaderIP: "1.2.3.4",
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
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta, ZipManifest: manifest,
		Reader: strReader("encrypted payload"), UploaderIP: "1.2.3.4",
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

func TestMissingEncryptionModeRejected(t *testing.T) {
	u, _, dir := newUploader(t, 1<<30)
	manifest := `[{"name":"note.txt","size":5,"type":"text/plain"}]`
	_, err := u.Do(upload.Request{
		Title: "plain", Visibility: "public", ExpiryHours: "6",
		DownloadPassword: "password", ZipManifest: manifest,
		Reader: strReader("encrypted payload"), UploaderIP: "1.2.3.4",
	})
	if !errors.Is(err, upload.ErrEncryptionRequired) {
		t.Fatalf("expected ErrEncryptionRequired, got %v", err)
	}
	if n := countBlobs(t, dir); n != 0 {
		t.Fatalf("payload without encryption mode was written: %d blobs", n)
	}
}

func TestTooShortCiphertextRejectedAndRemoved(t *testing.T) {
	u, _, dir := newUploader(t, 1<<30)
	_, err := u.Do(upload.Request{
		Title: "short", Visibility: "public", ExpiryHours: "6",
		DownloadPassword: "password", EncryptedFlag: "1", CipherMeta: testCipherMeta,
		Reader: strReader("short"), UploaderIP: "1.2.3.4",
	})
	if !errors.Is(err, upload.ErrEncryptionRequired) {
		t.Fatalf("expected ErrEncryptionRequired, got %v", err)
	}
	if n := countBlobs(t, dir); n != 0 {
		t.Fatalf("short ciphertext survived rejection: %d blobs", n)
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

type blockingReader struct {
	started chan struct{}
	release chan struct{}
	done    bool
}

func (r *blockingReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	close(r.started)
	<-r.release
	return copy(p, []byte("concurrent upload")), nil
}
