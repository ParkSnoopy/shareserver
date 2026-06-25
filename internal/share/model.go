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

// IsExpired reports whether an optional expiry timestamp is at or before now.
func IsExpired(exp sql.NullString, now time.Time) bool {
	if !exp.Valid || exp.String == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, exp.String)
	return err == nil && !t.After(now.UTC())
}

// Status collapses purge and expiry metadata into active, expired, or purged.
func Status(s Share, now time.Time) string {
	if s.PurgedAt.Valid {
		return "purged"
	}
	if IsExpired(s.ExpiresAt, now) {
		return "expired"
	}
	return "active"
}
