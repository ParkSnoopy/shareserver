package share

import (
	"database/sql"
	"time"
)

// Share is the storage-facing view of upload metadata and blob location.
type Share struct {
	ID, Title, Visibility, PrivateKeyHash string
	Encrypted                             bool
	CipherMeta, ZipManifest               string
	Size                                  int64
	BlobPath, BlobSHA256, UploaderIP      string
	ExpiresAt                             sql.NullString
	CreatedAt                             string
	PurgedAt                              sql.NullString
}

const (
	StatusActive  = "active"
	StatusExpired = "expired"
	StatusPurged  = "purged"
)

// purgeGrace is the retention window after expiry before a share's blob and
// metadata are purged. It gives the uploader a chance to retrieve a share
// shortly after it expires.
const purgeGrace = 24 * time.Hour

// ActiveRule is the single in-process definition of an active Share:
// non-purged and not expired at one instant.
type ActiveRule struct {
	now        time.Time
	encodedNow string
}

// ActiveAt fixes the active-Share rule to one UTC instant so Go classification
// and store predicates use the same timestamp.
func ActiveAt(now time.Time) ActiveRule {
	n := now.UTC()
	return ActiveRule{now: n, encodedNow: n.Format(time.RFC3339Nano)}
}

// IsExpired reports whether an optional expiry timestamp is at or before now.
func (r ActiveRule) IsExpired(exp sql.NullString) bool {
	if !exp.Valid || exp.String == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, exp.String)
	return err == nil && !t.After(r.now)
}

// IsActive reports whether the Share is visible to public/blob paths.
func (r ActiveRule) IsActive(s Share) bool {
	return !s.PurgedAt.Valid && !r.IsExpired(s.ExpiresAt)
}

// Status collapses purge and expiry metadata into active, expired, or purged.
func (r ActiveRule) Status(s Share) string {
	if s.PurgedAt.Valid {
		return StatusPurged
	}
	if r.IsExpired(s.ExpiresAt) {
		return StatusExpired
	}
	return StatusActive
}

// IsPurgeable reports whether an expired share has passed the purge grace
// window and should have its blob and metadata removed. A share is purgeable
// only if it is expired AND its expiry is older than purgeGrace. The grace
// window lives here — in the rule — not in the cleanup caller.
func (r ActiveRule) IsPurgeable(exp sql.NullString) bool {
	if !r.IsExpired(exp) {
		return false
	}
	if !exp.Valid || exp.String == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, exp.String)
	if err != nil {
		return false
	}
	return !t.After(r.now.Add(-purgeGrace))
}
