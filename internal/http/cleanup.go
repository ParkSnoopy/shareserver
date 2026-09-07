package httpx

import (
	"context"
	"log"
	"shareserver/internal/ent/session"
	"shareserver/internal/storage"
	"time"
)

// StartCleanup reconciles storage at boot, then runs daily purge and session cleanup.
func (h *Handler) StartCleanup() {
	unprotected := h.integrity().PurgeUnprotected()
	r := h.ReconcileBlobStore()
	n := h.PurgeExpired()
	if r.MissingFiles > 0 || r.OrphanFiles > 0 || n > 0 || unprotected > 0 {
		log.Printf("storage cleanup done count=%d unprotected=%d missing_files=%d orphan_files=%d", n, unprotected, r.MissingFiles, r.OrphanFiles)
	}
	go func() {
		for {
			d := h.nextMidnight()
			time.Sleep(time.Until(d))
			unprotected := h.integrity().PurgeUnprotected()
			r := h.ReconcileBlobStore()
			n := h.PurgeExpired()
			sc := h.CleanExpiredSessions()
			log.Printf("purge done count=%d unprotected=%d sessions=%d missing_files=%d orphan_files=%d", n, unprotected, sc, r.MissingFiles, r.OrphanFiles)
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
	return h.integrity().Purge(time.Now().UTC())
}

// ReconcileResult reports how many metadata rows or files storage repair removed.
type ReconcileResult = storage.ReconcileResult

// ReconcileBlobStore keeps blob storage and metadata consistent:
// rows whose blob files were removed are deleted, and blob files with no row
// are removed from disk.
func (h *Handler) ReconcileBlobStore() ReconcileResult {
	return h.integrity().Reconcile()
}

func (h *Handler) integrity() *storage.Integrity {
	if h.Integrity != nil {
		return h.Integrity
	}
	return storage.NewIntegrity(h.A.C.BlobDir, h.Store)
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
