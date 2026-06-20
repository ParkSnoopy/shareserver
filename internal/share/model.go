package share

import (
	"database/sql"
	"time"
)

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

func Scan(rows interface{ Scan(dest ...any) error }) (Share, error) {
	var s Share
	var enc int
	err := rows.Scan(&s.ID, &s.Title, &s.Visibility, &s.PrivateKeyHash, &enc, &s.CipherMeta, &s.ZipManifest, &s.Size, &s.BlobPath, &s.BlobSHA256, &s.UploaderIP, &s.ExpiresAt, &s.CreatedAt, &s.PurgedAt)
	s.Encrypted = enc == 1
	return s, err
}

func IsExpired(exp sql.NullString, now time.Time) bool {
	if !exp.Valid || exp.String == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, exp.String)
	return err == nil && !t.After(now.UTC())
}

func Status(s Share, now time.Time) string {
	if s.PurgedAt.Valid {
		return "purged"
	}
	if IsExpired(s.ExpiresAt, now) {
		return "expired"
	}
	return "active"
}
