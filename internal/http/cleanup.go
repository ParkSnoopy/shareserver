package httpx

import (
	"log"
	"shareserver/internal/share"
	"time"
)

func (h *Handler) StartCleanup() {
	go func() {
		for {
			d := h.nextMidnight()
			time.Sleep(time.Until(d))
			n := h.PurgeExpired()
			sc := h.CleanExpiredSessions()
			log.Printf("purge done count=%d sessions=%d", n, sc)
		}
	}()
}
func (h *Handler) nextMidnight() time.Time {
	now := time.Now().In(h.A.C.TZ)
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, h.A.C.TZ)
}
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
		purgeOne(s.BlobPath)
		_ = h.Store.MarkPurged(s.ID)
		count++
	}
	return count
}

// CleanExpiredSessions removes session rows past their expiry. Bounds the
// sessions table against anonymous-visit bloat (every visitor gets a row).
func (h *Handler) CleanExpiredSessions() int64 {
	res, err := h.A.DB.Exec(`delete from sessions where expires_at <= ?`, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}
