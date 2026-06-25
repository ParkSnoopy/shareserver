package httpx

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"shareserver/internal/ent/session"
	"shareserver/internal/share"
	"time"
)

// StartCleanup reconciles storage at boot, then runs daily purge and session cleanup.
func (h *Handler) StartCleanup() {
	go func() {
		r := h.ReconcileBlobStore()
		if r.MissingFiles > 0 || r.OrphanFiles > 0 {
			log.Printf("storage reconcile done missing_files=%d orphan_files=%d", r.MissingFiles, r.OrphanFiles)
		}
		for {
			d := h.nextMidnight()
			time.Sleep(time.Until(d))
			r := h.ReconcileBlobStore()
			n := h.PurgeExpired()
			sc := h.CleanExpiredSessions()
			log.Printf("purge done count=%d sessions=%d missing_files=%d orphan_files=%d", n, sc, r.MissingFiles, r.OrphanFiles)
		}
	}()
}

// nextMidnight returns the next maintenance boundary in the configured timezone.
func (h *Handler) nextMidnight() time.Time {
	now := time.Now().In(h.A.C.TZ)
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, h.A.C.TZ)
}

// PurgeExpired deletes blob files and rows after an expired share passes its grace period.
func (h *Handler) PurgeExpired() int {
	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour)
	count := 0
	for _, s := range h.Store.Purgeable() {
		if !share.IsExpired(s.ExpiresAt, now) {
			continue
		}
		exp, _ := time.Parse(time.RFC3339Nano, s.ExpiresAt.String)
		if exp.After(cutoff) {
			continue
		}
		if err := purgeOne(s.BlobPath); err != nil && !os.IsNotExist(err) {
			continue
		}
		_ = h.Store.Delete(s.ID)
		count++
	}
	return count
}

// ReconcileResult reports how many metadata rows or files storage repair removed.
type ReconcileResult struct {
	MissingFiles int
	OrphanFiles  int
}

// ReconcileBlobStore keeps blob storage and metadata consistent:
// rows whose blob files were removed are deleted, and blob files with no row
// are removed from disk.
func (h *Handler) ReconcileBlobStore() ReconcileResult {
	result := ReconcileResult{}
	known := map[string]struct{}{}
	for _, s := range h.Store.All() {
		blobPath := filepath.Clean(s.BlobPath)
		if _, err := os.Stat(blobPath); err != nil {
			if os.IsNotExist(err) {
				_ = h.Store.Delete(s.ID)
				result.MissingFiles++
			}
			continue
		}
		known[absPath(blobPath)] = struct{}{}
	}
	_ = filepath.WalkDir(h.A.C.BlobDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		blobPath := filepath.Clean(path)
		if _, ok := known[absPath(blobPath)]; ok {
			return nil
		}
		if err := os.Remove(blobPath); err == nil {
			result.OrphanFiles++
		}
		return nil
	})
	return result
}

// absPath normalizes paths before comparing registered blobs with disk entries.
func absPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return abs
}

// CleanExpiredSessions removes session rows past their expiry. Bounds the
// sessions table against anonymous-visit bloat (every visitor gets a row).
func (h *Handler) CleanExpiredSessions() int64 {
	n, err := h.A.DB.Session.Delete().
		Where(session.ExpiresAtLTE(time.Now().UTC().Format(time.RFC3339Nano))).
		Exec(context.Background())
	if err != nil {
		return 0
	}
	return int64(n)
}
