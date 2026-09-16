package httpx

import (
	"context"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	"shareserver/internal/ent"
	"shareserver/internal/ent/loginfailureevent"
)

const (
	downloadFailurePrefix = "download:"
	downloadFailureLimit  = 10
	downloadFailureWindow = time.Minute
	downloadResponseDelay = 2 * time.Second
	downloadTimestamp     = "2006-01-02T15:04:05.000000000Z07:00"
	downloadBanBase       = 24 * time.Hour
	downloadBanJitterMin  = -3600 * time.Second
	downloadBanJitterSpan = 21601
)

type downloadProtection struct {
	mu     sync.Mutex
	db     *ent.Client
	jitter func() time.Duration
}

// newDownloadProtection creates a persistent per-IP failed-password guard.
func newDownloadProtection(db *ent.Client) *downloadProtection {
	return &downloadProtection{
		db: db,
		jitter: func() time.Duration {
			return downloadBanJitterMin + time.Duration(rand.IntN(downloadBanJitterSpan))*time.Second
		},
	}
}

// bannedUntil reads the scoped durable ban and removes it after expiry.
func (p *downloadProtection) bannedUntil(ctx context.Context, ip string, now time.Time) (time.Time, bool) {
	ban, err := p.db.IpBan.Get(ctx, downloadFailurePrefix+ip)
	if ent.IsNotFound(err) {
		return time.Time{}, false
	}
	if err != nil {
		return now.Add(downloadBanBase), true
	}
	until, err := time.Parse(time.RFC3339Nano, ban.BannedUntil)
	if err != nil {
		return now.Add(downloadBanBase), true
	}
	if until.After(now) {
		return until, true
	}
	_ = p.db.IpBan.DeleteOneID(ban.ID).Exec(ctx)
	return time.Time{}, false
}

// recordFailure persists one failed password request and creates a scoped ban
// only after the rolling one-minute count exceeds ten.
func (p *downloadProtection) recordFailure(ctx context.Context, ip string, now time.Time) (time.Time, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if until, banned := p.bannedUntil(ctx, ip, now); banned {
		return until, true, nil
	}
	failureKey := downloadFailurePrefix + ip
	cutoff := now.Add(-downloadFailureWindow).UTC().Format(downloadTimestamp)
	_, err := p.db.LoginFailureEvent.Create().
		SetIP(failureKey).
		SetHappenedAt(now.UTC().Format(downloadTimestamp)).
		Save(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	if _, err := p.db.LoginFailureEvent.Delete().
		Where(
			loginfailureevent.IPHasPrefix(downloadFailurePrefix),
			loginfailureevent.HappenedAtLTE(cutoff),
		).
		Exec(ctx); err != nil {
		return time.Time{}, false, err
	}
	count, err := p.db.LoginFailureEvent.Query().
		Where(
			loginfailureevent.IP(failureKey),
			loginfailureevent.HappenedAtGT(cutoff),
		).
		Count(ctx)
	if err != nil || count <= downloadFailureLimit {
		return time.Time{}, false, err
	}
	until := now.Add(downloadBanBase + p.jitter()).UTC()
	banID := failureKey
	if _, err := p.db.IpBan.Get(ctx, banID); err == nil {
		err = p.db.IpBan.UpdateOneID(banID).SetBannedUntil(until.Format(time.RFC3339Nano)).Exec(ctx)
	} else if ent.IsNotFound(err) {
		_, err = p.db.IpBan.Create().SetID(banID).SetBannedUntil(until.Format(time.RFC3339Nano)).Save(ctx)
	}
	return until, err == nil, err
}

// retryAfterSeconds reports a positive whole-second wait for HTTP Retry-After.
func retryAfterSeconds(until, now time.Time) string {
	wait := until.Sub(now)
	seconds := int64((wait + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10)
}
