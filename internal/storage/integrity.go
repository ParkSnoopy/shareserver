package storage

import (
	"os"
	"path/filepath"
	"time"

	"shareserver/internal/share"
)

// Integrity owns consistency between Share metadata and stored blobs.
type Integrity struct {
	BlobDir string
	Store   *share.Store
}

// ReconcileResult reports storage inconsistencies repaired in one pass.
type ReconcileResult struct {
	MissingFiles int
	OrphanFiles  int
}

// NewIntegrity binds Share metadata and blob storage behind one module.
func NewIntegrity(blobDir string, store *share.Store) *Integrity {
	return &Integrity{BlobDir: blobDir, Store: store}
}

// Remove deletes a Share blob before deleting its metadata row. Missing blobs
// already match the desired state; other filesystem errors preserve metadata
// as a retry handle.
func (i *Integrity) Remove(sh share.Share) error {
	if err := os.Remove(filepath.Clean(sh.BlobPath)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return i.Store.Delete(sh.ID)
}

// Purge removes Shares that are expired at now.
func (i *Integrity) Purge(now time.Time) int {
	rule := share.ActiveAt(now)
	count := 0
	for _, sh := range i.Store.WithExpiry() {
		if !rule.IsPurgeable(sh.ExpiresAt) {
			continue
		}
		if err := i.Remove(sh); err == nil {
			count++
		}
	}
	return count
}

// Reconcile removes metadata for missing blobs and unregistered files from the
// configured blob directory.
func (i *Integrity) Reconcile() ReconcileResult {
	result := ReconcileResult{}
	known := map[string]struct{}{}
	for _, sh := range i.Store.All() {
		blobPath := filepath.Clean(sh.BlobPath)
		if _, err := os.Stat(blobPath); err != nil {
			if os.IsNotExist(err) {
				if err := i.Remove(sh); err == nil {
					result.MissingFiles++
				}
			}
			continue
		}
		known[absolutePath(blobPath)] = struct{}{}
	}
	_ = filepath.WalkDir(i.BlobDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		blobPath := filepath.Clean(path)
		if _, ok := known[absolutePath(blobPath)]; ok {
			return nil
		}
		if err := os.Remove(blobPath); err == nil {
			result.OrphanFiles++
		}
		return nil
	})
	return result
}

func absolutePath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return absolute
}
