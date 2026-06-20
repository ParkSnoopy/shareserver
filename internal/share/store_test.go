package share

import (
	"database/sql"
	"testing"
	"time"

	"shareserver/internal/db"
)

func newTestStore(t *testing.T) (*Store, *sql.DB, func()) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return NewStore(d), d, func() { _ = d.Close() }
}

func mustInsert(t *testing.T, s *Store, sh Share) {
	t.Helper()
	if err := s.Insert(sh); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func sampleShare(id, vis string, exp string) Share {
	var expN sql.NullString
	if exp != "" {
		expN = sql.NullString{String: exp, Valid: true}
	}
	return Share{
		ID: id, Title: "t-" + id, Visibility: vis,
		Encrypted: false, Size: 100, BlobPath: "/tmp/" + id + ".blob",
		BlobSHA256: "sum-" + id, UploaderIP: "1.2.3.4",
		ExpiresAt: expN,
	}
}

func TestStoreGetMissing(t *testing.T) {
	s, _, cleanup := newTestStore(t)
	defer cleanup()
	if _, ok := s.Get("nope"); ok {
		t.Fatal("Get missing should return ok=false")
	}
}

func TestStoreInsertGetRoundTrip(t *testing.T) {
	s, _, cleanup := newTestStore(t)
	defer cleanup()
	want := sampleShare("id1", "public", futureTS(1*time.Hour))
	mustInsert(t, s, want)
	got, ok := s.Get("id1")
	if !ok {
		t.Fatal("Get failed after Insert")
	}
	if got.Title != want.Title || got.Visibility != want.Visibility || got.Size != want.Size {
		t.Fatalf("roundtrip mismatch: got %+v", got)
	}
	if !got.ExpiresAt.Valid || got.ExpiresAt.String != want.ExpiresAt.String {
		t.Fatalf("expiry mismatch: got %+v want %+v", got.ExpiresAt, want.ExpiresAt)
	}
}

func TestStoreListPublicExcludesExpiredAndPrivate(t *testing.T) {
	s, _, cleanup := newTestStore(t)
	defer cleanup()
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)
	mustInsert(t, s, sampleShare("pub-live", "public", futureTS(1*time.Hour)))
	mustInsert(t, s, sampleShare("pub-expired", "public", pastTS(1*time.Hour)))
	mustInsert(t, s, sampleShare("priv-live", "private", futureTS(1*time.Hour)))

	list := s.ListPublic(nowStr)
	if len(list) != 1 {
		t.Fatalf("ListPublic want 1, got %d: %+v", len(list), list)
	}
	if list[0].ID != "pub-live" {
		t.Fatalf("ListPublic returned wrong share: %+v", list[0])
	}
}

func TestStoreListByKeyMatchesHash(t *testing.T) {
	s, _, cleanup := newTestStore(t)
	defer cleanup()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustInsert(t, s, sampleShare("priv1", "private", futureTS(1*time.Hour)))
	// overwrite key hash to a known value
	if _, err := s.DB.Exec(`update shares set private_key_hash=? where id=?`, "hash-X", "priv1"); err != nil {
		t.Fatal(err)
	}
	list := s.ListByKey(now, "hash-X")
	if len(list) != 1 || list[0].ID != "priv1" {
		t.Fatalf("ListByKey want [priv1], got %+v", list)
	}
	list = s.ListByKey(now, "hash-other")
	if len(list) != 0 {
		t.Fatalf("ListByKey wrong hash should return 0, got %+v", list)
	}
}

func TestStoreListAllIncludesEverything(t *testing.T) {
	s, _, cleanup := newTestStore(t)
	defer cleanup()
	mustInsert(t, s, sampleShare("a", "public", futureTS(1*time.Hour)))
	mustInsert(t, s, sampleShare("b", "private", pastTS(1*time.Hour))) // expired, not purged
	mustInsert(t, s, sampleShare("c", "public", futureTS(1*time.Hour)))
	list := s.ListAll()
	if len(list) != 3 {
		t.Fatalf("ListAll want 3 (incl expired), got %d", len(list))
	}
}
func TestStorePurgeableOnlyExpiryNonPurged(t *testing.T) {
	s, _, cleanup := newTestStore(t)
	defer cleanup()
	mustInsert(t, s, sampleShare("exp", "public", pastTS(1*time.Hour)))    // expired, has expiry
	mustInsert(t, s, sampleShare("live", "public", futureTS(1*time.Hour))) // not expired, has expiry
	mustInsert(t, s, sampleShare("noexp", "public", ""))                   // no expiry -> excluded
	list := s.Purgeable()
	if len(list) != 2 {
		t.Fatalf("Purgeable want 2 (both with expiry), got %d: %+v", len(list), list)
	}
	ids := map[string]bool{}
	for _, sh := range list {
		ids[sh.ID] = true
	}
	if !ids["exp"] || !ids["live"] {
		t.Fatalf("Purgeable missing exp/live, got %+v", list)
	}
	if ids["noexp"] {
		t.Fatal("Purgeable included no-expiry share")
	}
}
func TestStoreCountsAndMarkPurged(t *testing.T) {
	s, _, cleanup := newTestStore(t)
	defer cleanup()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	mustInsert(t, s, sampleShare("live", "public", futureTS(1*time.Hour)))
	mustInsert(t, s, sampleShare("exp", "public", pastTS(1*time.Hour)))
	if s.CountActive(now) != 1 {
		t.Fatalf("CountActive want 1, got %d", s.CountActive(now))
	}
	if s.CountExpired(now) != 1 {
		t.Fatalf("CountExpired want 1, got %d", s.CountExpired(now))
	}
	if s.CountPurged() != 0 {
		t.Fatalf("CountPurged want 0, got %d", s.CountPurged())
	}
	if err := s.MarkPurged("exp"); err != nil {
		t.Fatal(err)
	}
	if s.CountPurged() != 1 {
		t.Fatalf("CountPurged after mark want 1, got %d", s.CountPurged())
	}
	// purged share should not appear in Purgeable anymore
	list := s.Purgeable()
	for _, sh := range list {
		if sh.ID == "exp" {
			t.Fatal("purged share still in Purgeable")
		}
	}
}

func TestStoreDeleteRemovesRow(t *testing.T) {
	s, _, cleanup := newTestStore(t)
	defer cleanup()
	mustInsert(t, s, sampleShare("del", "public", futureTS(1*time.Hour)))
	if err := s.Delete("del"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("del"); ok {
		t.Fatal("Delete did not remove row")
	}
}

func futureTS(d time.Duration) string {
	return time.Now().UTC().Add(d).Format(time.RFC3339Nano)
}
func pastTS(d time.Duration) string {
	return time.Now().UTC().Add(-d).Format(time.RFC3339Nano)
}
