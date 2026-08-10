package storage

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"shareserver/internal/share"
)

// Integrity owns consistency between Share metadata and stored blobs.
type Integrity struct {
	BlobDir string
	Store   *share.Store
	mu      sync.Mutex
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

// Lock excludes reconciliation and removal while a caller completes a
// multi-step blob/database mutation. The returned function releases the lock.
func (i *Integrity) Lock() func() {
	i.mu.Lock()
	return i.mu.Unlock
}

// Remove deletes a Share blob before deleting its metadata row. Missing or
// non-regular blob paths have no valid Share blob to preserve, so their stale
// metadata is deleted; other filesystem errors preserve metadata for retry.
func (i *Integrity) Remove(sh share.Share) error {
	unlock := i.Lock()
	defer unlock()
	return i.remove(sh)
}

func (i *Integrity) remove(sh share.Share) error {
	blobPath := filepath.Clean(sh.BlobPath)
	info, err := os.Lstat(blobPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && info.Mode().IsRegular() {
		if err := os.Remove(blobPath); err != nil {
			return err
		}
	}
	return i.Store.Delete(sh.ID)
}

// Purge removes Shares that are expired at now.
func (i *Integrity) Purge(now time.Time) int {
	unlock := i.Lock()
	defer unlock()
	rule := share.ActiveAt(now)
	count := 0
	for _, sh := range i.Store.WithExpiry() {
		if !rule.IsPurgeable(sh.ExpiresAt) {
			continue
		}
		if err := i.remove(sh); err == nil {
			count++
		}
	}
	return count
}

// Reconcile removes metadata for missing blobs and unregistered files from the
// configured blob directory.
func (i *Integrity) Reconcile() ReconcileResult {
	unlock := i.Lock()
	defer unlock()
	result := ReconcileResult{}
	known := map[string]struct{}{}
	for _, sh := range i.Store.All() {
		blobPath := filepath.Clean(sh.BlobPath)
		info, err := os.Lstat(blobPath)
		if err != nil || !info.Mode().IsRegular() {
			if err == nil || os.IsNotExist(err) {
				if err := i.remove(sh); err == nil {
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
