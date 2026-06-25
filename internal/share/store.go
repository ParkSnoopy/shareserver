package share

import (
	"context"
	"database/sql"

	"shareserver/internal/db"
	"shareserver/internal/ent"
	entshare "shareserver/internal/ent/share"
)

// Store is the deep module behind every shares query.
// Schema, null mapping, and selection rules live here; callers learn
// List/Get/Insert/etc, never SQL details.
type Store struct {
	Client *ent.Client
}

// NewStore binds the share query module to an Ent client.
func NewStore(client *ent.Client) *Store { return &Store{Client: client} }

// Get returns the share with the given id. ok is false if absent.
func (s *Store) Get(id string) (Share, bool) {
	row, err := s.Client.Share.Get(context.Background(), id)
	if err != nil {
		return Share{}, false
	}
	return fromEnt(row), true
}

// ListPublic returns up to 100 non-purged, non-expired public shares.
func (s *Store) ListPublic(now string) []Share {
	return s.query(s.Client.Share.Query().
		Where(
			entshare.PurgedAtIsNil(),
			entshare.Or(entshare.ExpiresAtIsNil(), entshare.ExpiresAtGT(now)),
			entshare.VisibilityEQ("public"),
		).
		Order(ent.Desc(entshare.FieldCreatedAt)).
		Limit(100))
}

// ListByKey returns up to 100 non-purged, non-expired shares matching keyHash.
// Used by the private-key lookup flow.
func (s *Store) ListByKey(now, keyHash string) []Share {
	return s.query(s.Client.Share.Query().
		Where(
			entshare.PurgedAtIsNil(),
			entshare.Or(entshare.ExpiresAtIsNil(), entshare.ExpiresAtGT(now)),
			entshare.PrivateKeyHashEQ(keyHash),
		).
		Order(ent.Desc(entshare.FieldCreatedAt)).
		Limit(100))
}

// ListAll returns up to 300 shares of any visibility/status, newest first.
// Admin view; includes expired-but-not-purged.
func (s *Store) ListAll() []Share {
	return s.query(s.Client.Share.Query().
		Order(ent.Desc(entshare.FieldCreatedAt)).
		Limit(300))
}

// All returns every share row for storage reconciliation.
func (s *Store) All() []Share {
	return s.query(s.Client.Share.Query())
}

// Purgeable returns non-purged shares that have an expiry. The 24h-grace
// and expired checks are applied by the caller (cleanup policy).
func (s *Store) Purgeable() []Share {
	return s.query(s.Client.Share.Query().
		Where(entshare.PurgedAtIsNil(), entshare.ExpiresAtNotNil()))
}

// CountActive counts non-purged, non-expired shares.
func (s *Store) CountActive(now string) int {
	return s.count(s.Client.Share.Query().
		Where(
			entshare.PurgedAtIsNil(),
			entshare.Or(entshare.ExpiresAtIsNil(), entshare.ExpiresAtGT(now)),
		))
}

// CountExpired counts non-purged shares past expiry.
func (s *Store) CountExpired(now string) int {
	return s.count(s.Client.Share.Query().
		Where(entshare.PurgedAtIsNil(), entshare.ExpiresAtLTE(now)))
}

// CountPurged counts shares with purged_at set.
func (s *Store) CountPurged() int {
	return s.count(s.Client.Share.Query().Where(entshare.PurgedAtNotNil()))
}

// Insert writes a new share row. created_at is set server-side.
func (s *Store) Insert(sh Share) error {
	create := s.Client.Share.Create().
		SetID(sh.ID).
		SetTitle(sh.Title).
		SetVisibility(sh.Visibility).
		SetNillablePrivateKeyHash(nonEmptyString(sh.PrivateKeyHash)).
		SetEncrypted(sh.Encrypted).
		SetCipherMeta(sh.CipherMeta).
		SetZipManifest(sh.ZipManifest).
		SetSize(sh.Size).
		SetBlobPath(sh.BlobPath).
		SetBlobSha256(sh.BlobSHA256).
		SetUploaderIP(sh.UploaderIP).
		SetNillableExpiresAt(nullStringPtr(sh.ExpiresAt)).
		SetCreatedAt(db.Now()).
		SetNillablePurgedAt(nullStringPtr(sh.PurgedAt))
	_, err := create.Save(context.Background())
	return err
}

// MarkPurged records purge time for a share.
func (s *Store) MarkPurged(id string) error {
	_, err := s.Client.Share.Update().
		Where(entshare.ID(id)).
		SetPurgedAt(db.Now()).
		Save(context.Background())
	return err
}

// Delete removes the share row entirely (blob deletion is the caller's job).
func (s *Store) Delete(id string) error {
	_, err := s.Client.Share.Delete().Where(entshare.ID(id)).Exec(context.Background())
	return err
}

// query executes a share query and maps Ent rows into domain shares.
func (s *Store) query(q *ent.ShareQuery) []Share {
	rows, err := q.All(context.Background())
	if err != nil {
		return nil
	}
	list := make([]Share, 0, len(rows))
	for _, row := range rows {
		list = append(list, fromEnt(row))
	}
	return list
}

// count executes a share count and returns zero on storage errors.
func (s *Store) count(q *ent.ShareQuery) int {
	n, err := q.Count(context.Background())
	if err != nil {
		return 0
	}
	return n
}

// fromEnt converts generated Ent rows into the hand-written Share model.
func fromEnt(row *ent.Share) Share {
	return Share{
		ID:             row.ID,
		Title:          row.Title,
		Visibility:     row.Visibility,
		PrivateKeyHash: stringValue(row.PrivateKeyHash),
		Encrypted:      row.Encrypted,
		CipherMeta:     row.CipherMeta,
		ZipManifest:    row.ZipManifest,
		Size:           row.Size,
		BlobPath:       row.BlobPath,
		BlobSHA256:     row.BlobSha256,
		UploaderIP:     row.UploaderIP,
		ExpiresAt:      nullString(row.ExpiresAt),
		CreatedAt:      row.CreatedAt,
		PurgedAt:       nullString(row.PurgedAt),
	}
}

// stringValue turns optional Ent strings into empty-string domain fields.
func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// nullString maps optional Ent strings to sql.NullString for legacy callers.
func nullString(v *string) sql.NullString {
	if v == nil || *v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}

// nullStringPtr maps optional legacy strings back into Ent setters.
func nullStringPtr(v sql.NullString) *string {
	if !v.Valid || v.String == "" {
		return nil
	}
	return &v.String
}

// nonEmptyString lets Ent omit empty optional string fields.
func nonEmptyString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
