package share

import (
	"context"
	"database/sql"

	"shareserver/internal/db"
	"shareserver/internal/ent"
	"shareserver/internal/ent/predicate"
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

// Predicate returns the Ent predicate for active shares at this rule's instant.
func (r ActiveRule) Predicate() predicate.Share {
	return entshare.And(
		entshare.PurgedAtIsNil(),
		entshare.Or(entshare.ExpiresAtIsNil(), entshare.ExpiresAtGT(r.encodedNow)),
	)
}

// ExpiredPredicate returns the Ent predicate for non-purged expired shares.
func (r ActiveRule) ExpiredPredicate() predicate.Share {
	return entshare.And(entshare.PurgedAtIsNil(), entshare.ExpiresAtLTE(r.encodedNow))
}

// Get returns the share with the given id. ok is false if absent.
func (s *Store) Get(id string) (Share, bool) {
	row, err := s.Client.Share.Get(context.Background(), id)
	if err != nil {
		return Share{}, false
	}
	return fromEnt(row), true
}

// ListPublic returns up to 100 active public shares.
func (s *Store) ListPublic(active ActiveRule) []Share {
	return s.query(s.Client.Share.Query().
		Where(active.Predicate(), entshare.VisibilityEQ("public")).
		Order(ent.Desc(entshare.FieldCreatedAt)).
		Limit(100))
}

// ListByKey returns up to 100 active shares matching keyHash.
// Used by the private-key lookup flow.
func (s *Store) ListByKey(active ActiveRule, keyHash string) []Share {
	return s.query(s.Client.Share.Query().
		Where(active.Predicate(), entshare.PrivateKeyHashEQ(keyHash)).
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

// WithExpiry returns non-purged shares that have an expiry set. The caller
// applies the purge rule (ActiveRule.IsPurgeable) to decide which are past
// the grace window. Renamed from Purgeable — that name implied the result
// was already purgeable, but it includes live shares too.
func (s *Store) WithExpiry() []Share {
	return s.query(s.Client.Share.Query().
		Where(entshare.PurgedAtIsNil(), entshare.ExpiresAtNotNil()))
}

// CountActive counts active shares.
func (s *Store) CountActive(active ActiveRule) int {
	return s.count(s.Client.Share.Query().Where(active.Predicate()))
}

// CountExpired counts non-purged shares past expiry.
func (s *Store) CountExpired(active ActiveRule) int {
	return s.count(s.Client.Share.Query().Where(active.ExpiredPredicate()))
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
