package share

import (
	"os"
	"path/filepath"
)

// Remover owns the two-part removal invariant for a Share: blob first,
// metadata only after the blob is gone or already missing.
type Remover struct {
	Store *Store
}

// NewRemover binds the share removal module to metadata storage.
func NewRemover(store *Store) Remover { return Remover{Store: store} }

// Remove deletes a Share's blob and then its metadata row. A missing blob is
// already consistent with the desired end state, so metadata can still be
// removed; other blob errors leave the row as a retry handle.
func (r Remover) Remove(sh Share) error {
	if err := RemoveBlob(sh.BlobPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return r.Store.Delete(sh.ID)
}

// RemoveByID looks up and removes a Share. Missing rows are already removed.
func (r Remover) RemoveByID(id string) (bool, error) {
	sh, ok := r.Store.Get(id)
	if !ok {
		return false, nil
	}
	return true, r.Remove(sh)
}

// RemoveBlob deletes one blob path after cleaning it.
func RemoveBlob(path string) error { return os.Remove(filepath.Clean(path)) }

// RemoveBlobBestEffort deletes a blob path when rolling back a partial write.
func RemoveBlobBestEffort(path string) { _ = RemoveBlob(path) }
