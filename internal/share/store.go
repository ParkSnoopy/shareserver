package share

import (
	"database/sql"
	"shareserver/internal/db"
)

// cols is the canonical shares row, kept in sync with Scan.
const cols = `id,title,visibility,private_key_hash,encrypted,cipher_meta,zip_manifest,size,blob_path,blob_sha256,uploader_ip,expires_at,created_at,purged_at`

// Store is the deep module behind every shares query.
// Schema, column order, and selection rules live here; callers learn
// List/Get/Insert/etc, never the SQL.
type Store struct {
	DB *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{DB: db} }

// Get returns the share with the given id. ok is false if absent.
func (s *Store) Get(id string) (Share, bool) {
	row := s.DB.QueryRow(`select `+cols+` from shares where id=?`, id)
	sh, err := Scan(row)
	return sh, err == nil
}

// ListPublic returns up to 100 non-purged, non-expired public shares.
func (s *Store) ListPublic(now string) []Share {
	q := `select ` + cols + ` from shares where purged_at is null and (expires_at is null or expires_at>?) and visibility='public' order by created_at desc limit 100`
	return query(s.DB, q, now)
}

// ListByKey returns up to 100 non-purged, non-expired shares matching keyHash.
// Used by the private-key lookup flow.
func (s *Store) ListByKey(now, keyHash string) []Share {
	q := `select ` + cols + ` from shares where purged_at is null and (expires_at is null or expires_at>?) and private_key_hash=? order by created_at desc limit 100`
	return query(s.DB, q, now, keyHash)
}

// ListAll returns up to 300 shares of any visibility/status, newest first.
// Admin view; includes expired-but-not-purged.
func (s *Store) ListAll() []Share {
	q := `select ` + cols + ` from shares order by created_at desc limit 300`
	return query(s.DB, q)
}

// Purgeable returns non-purged shares that have an expiry. The 24h-grace
// and expired checks are applied by the caller (cleanup policy).
func (s *Store) Purgeable() []Share {
	q := `select ` + cols + ` from shares where purged_at is null and expires_at is not null`
	return query(s.DB, q)
}

// CountActive counts non-purged, non-expired shares.
func (s *Store) CountActive(now string) int {
	var n int
	_ = s.DB.QueryRow(`select count(*) from shares where purged_at is null and (expires_at is null or expires_at>?)`, now).Scan(&n)
	return n
}

// CountExpired counts non-purged shares past expiry.
func (s *Store) CountExpired(now string) int {
	var n int
	_ = s.DB.QueryRow(`select count(*) from shares where purged_at is null and expires_at<=?`, now).Scan(&n)
	return n
}

// CountPurged counts shares with purged_at set.
func (s *Store) CountPurged() int {
	var n int
	_ = s.DB.QueryRow(`select count(*) from shares where purged_at is not null`).Scan(&n)
	return n
}

// Insert writes a new share row. created_at is set server-side.
func (s *Store) Insert(sh Share) error {
	enc := 0
	if sh.Encrypted {
		enc = 1
	}
	_, err := s.DB.Exec(
		`insert into shares(id,title,visibility,private_key_hash,encrypted,cipher_meta,zip_manifest,size,blob_path,blob_sha256,uploader_ip,expires_at,created_at) values(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sh.ID, sh.Title, sh.Visibility, sh.PrivateKeyHash, enc, sh.CipherMeta, sh.ZipManifest, sh.Size, sh.BlobPath, sh.BlobSHA256, sh.UploaderIP, sh.ExpiresAt, db.Now(),
	)
	return err
}

// MarkPurged records purge time for a share.
func (s *Store) MarkPurged(id string) error {
	_, err := s.DB.Exec(`update shares set purged_at=? where id=?`, db.Now(), id)
	return err
}

// Delete removes the share row entirely (blob deletion is the caller's job).
func (s *Store) Delete(id string) error {
	_, err := s.DB.Exec(`delete from shares where id=?`, id)
	return err
}

// query runs a select and scans all rows into Shares. Errors are swallowed to
// preserve prior handler behaviour (return what we have); a nil slice is
// returned on query failure.
func query(db *sql.DB, q string, args ...any) []Share {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var list []Share
	for rows.Next() {
		sh, err := Scan(rows)
		if err != nil {
			continue
		}
		list = append(list, sh)
	}
	return list
}
