package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"shareserver/internal/db"
	"shareserver/internal/ent"
)

const sessionDuration = 12 * time.Hour

// Session is the request-scoped browser session used for CSRF and admin state.
type Session struct {
	ID        string
	AdminID   int64
	CSRF      string
	Language  string
	ExpiresAt time.Time
}

// SessionLifecycle owns session row lifecycle policy; HTTP code owns cookies.
type SessionLifecycle interface {
	Get(ctx context.Context, sid string) (Session, bool)
	GetOrCreate(ctx context.Context, sid, language string) (Session, bool)
	Rotate(ctx context.Context, oldSID string, adminID int64) Session
	Delete(ctx context.Context, sid string)
}

// DBSessions stores browser sessions in SQLite through ent.
type DBSessions struct {
	DB  *ent.Client
	Now func() time.Time
}

// NewSessions creates the default SQLite-backed session lifecycle.
func NewSessions(client *ent.Client) *DBSessions {
	return &DBSessions{DB: client, Now: func() time.Time { return time.Now().UTC() }}
}

// Get returns one unexpired session without creating or mutating state.
func (s *DBSessions) Get(ctx context.Context, sid string) (Session, bool) {
	if sid == "" {
		return Session{}, false
	}
	row, err := s.DB.Session.Get(ctx, sid)
	if err != nil {
		return Session{}, false
	}
	sess := sessionFromRow(row)
	return sess, sess.ExpiresAt.After(s.now())
}

// GetOrCreate reuses a valid session, drops an expired row, or creates a new anonymous session.
func (s *DBSessions) GetOrCreate(ctx context.Context, sid, language string) (Session, bool) {
	now := s.now()
	if sid != "" {
		if sess, ok := s.Get(ctx, sid); ok {
			return sess, false
		}
		if _, err := s.DB.Session.Get(ctx, sid); err == nil {
			_ = s.DB.Session.DeleteOneID(sid).Exec(ctx)
		}
	}
	return s.create(ctx, now, 0, false, language), true
}

// Rotate deletes the previous session and creates a fresh admin session.
func (s *DBSessions) Rotate(ctx context.Context, oldSID string, adminID int64) Session {
	language := "en"
	if row, err := s.DB.Session.Get(ctx, oldSID); err == nil {
		language = row.Language
	}
	_ = s.DB.Session.DeleteOneID(oldSID).Exec(ctx)
	return s.create(ctx, s.now(), adminID, true, language)
}

// Delete removes a session row so the cookie can no longer authorize requests.
func (s *DBSessions) Delete(ctx context.Context, sid string) {
	_ = s.DB.Session.DeleteOneID(sid).Exec(ctx)
}

func (s *DBSessions) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *DBSessions) create(ctx context.Context, now time.Time, adminID int64, admin bool, language string) Session {
	if language == "" {
		language = "en"
	}
	sess := Session{ID: randHex(32), AdminID: adminID, CSRF: randHex(32), Language: language, ExpiresAt: now.Add(sessionDuration).UTC()}
	create := s.DB.Session.Create().
		SetID(sess.ID).
		SetCsrf(sess.CSRF).
		SetLanguage(sess.Language).
		SetCreatedAt(db.Now()).
		SetExpiresAt(sess.ExpiresAt.Format(time.RFC3339Nano))
	if admin {
		create.SetAdminID(adminID)
	}
	_, _ = create.Save(ctx)
	return sess
}

func sessionFromRow(row *ent.Session) Session {
	sess := Session{ID: row.ID, CSRF: row.Csrf, Language: row.Language}
	if row.AdminID != nil {
		sess.AdminID = *row.AdminID
	}
	if t, err := time.Parse(time.RFC3339Nano, row.ExpiresAt); err == nil {
		sess.ExpiresAt = t
	}
	return sess
}

// randHex returns cryptographic random hex for session IDs and CSRF tokens.
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
