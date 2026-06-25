package internaltest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReconcileDeletesRowsForMissingBlobFiles(t *testing.T) {
	h, _ := newTestHandler(t)
	id := "00000000-0000-0000-0000-000000000101"
	missing := filepath.Join(h.A.C.BlobDir, id+".blob")
	sh := sampleShare(id, "public", futureTS(time.Hour))
	sh.BlobPath = missing
	mustInsertShare(t, h.Store, sh)

	result := h.ReconcileBlobStore()
	if result.MissingFiles != 1 || result.OrphanFiles != 0 {
		t.Fatalf("unexpected reconcile result: %+v", result)
	}
	if _, ok := h.Store.Get(id); ok {
		t.Fatalf("share row survived missing blob reconciliation")
	}
}

func TestReconcileRemovesUnregisteredFilesWithoutRows(t *testing.T) {
	h, _ := newTestHandler(t)
	if err := os.MkdirAll(h.A.C.BlobDir, 0755); err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-0000-0000-000000000102"
	registered := filepath.Join(h.A.C.BlobDir, id+".stored")
	orphanBlob := filepath.Join(h.A.C.BlobDir, "orphan.blob")
	orphanText := filepath.Join(h.A.C.BlobDir, "notes.txt")
	for _, path := range []string{registered, orphanBlob, orphanText} {
		if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	sh := sampleShare(id, "public", futureTS(time.Hour))
	sh.BlobPath = registered
	mustInsertShare(t, h.Store, sh)

	result := h.ReconcileBlobStore()
	if result.MissingFiles != 0 || result.OrphanFiles != 2 {
		t.Fatalf("unexpected reconcile result: %+v", result)
	}
	if _, err := os.Stat(orphanBlob); !os.IsNotExist(err) {
		t.Fatalf("unregistered blob file still exists: %v", err)
	}
	if _, err := os.Stat(orphanText); !os.IsNotExist(err) {
		t.Fatalf("unregistered non-blob file still exists: %v", err)
	}
	if _, err := os.Stat(registered); err != nil {
		t.Fatalf("registered file was removed: %v", err)
	}
}

func TestPurgeExpiredDeletesBlobAndRow(t *testing.T) {
	h, _ := newTestHandler(t)
	id := "00000000-0000-0000-0000-000000000103"
	blob := filepath.Join(h.A.C.BlobDir, id+".blob")
	if err := os.MkdirAll(h.A.C.BlobDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blob, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	sh := sampleShare(id, "public", time.Now().UTC().Add(-25*time.Hour).Format(time.RFC3339Nano))
	sh.BlobPath = blob
	mustInsertShare(t, h.Store, sh)

	if n := h.PurgeExpired(); n != 1 {
		t.Fatalf("expected 1 purged share, got %d", n)
	}
	if _, err := os.Stat(blob); !os.IsNotExist(err) {
		t.Fatalf("expired blob still exists: %v", err)
	}
	if _, ok := h.Store.Get(id); ok {
		t.Fatalf("expired share row survived purge")
	}
}
