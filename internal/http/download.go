package httpx

import (
	"sync"
	"time"
)

type downloadRateLimiter struct {
	mu        sync.Mutex
	attempts  map[string][]time.Time
	limit     int
	window    time.Duration
	lastSweep time.Time
}

// newDownloadRateLimiter creates a per-IP rolling-window limiter.
func newDownloadRateLimiter(limit int, window time.Duration) *downloadRateLimiter {
	if limit < 1 {
		limit = 1
	}
	return &downloadRateLimiter{attempts: make(map[string][]time.Time), limit: limit, window: window}
}

// Allow consumes one attempt for an IP when its rolling window has capacity.
func (l *downloadRateLimiter) Allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := now.Add(-l.window)
	if l.lastSweep.IsZero() || now.Sub(l.lastSweep) >= l.window {
		for address, entries := range l.attempts {
			if len(entries) == 0 || entries[len(entries)-1].Before(cutoff) {
				delete(l.attempts, address)
			}
		}
		l.lastSweep = now
	}
	attempts := l.attempts[ip]
	first := 0
	for first < len(attempts) && attempts[first].Before(cutoff) {
		first++
	}
	attempts = attempts[first:]
	if len(attempts) >= l.limit {
		l.attempts[ip] = attempts
		return false
	}
	l.attempts[ip] = append(attempts, now)
	return true
}
