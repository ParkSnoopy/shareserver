package db

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"time"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, migrate(db)
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`create table if not exists admins (id integer primary key, username text not null unique, password_hash text not null, created_at text not null);`,
		`create table if not exists sessions (id text primary key, admin_id integer, csrf text not null, created_at text not null, expires_at text not null);`,
		`create table if not exists ip_bans (ip text primary key, banned_until text not null);`,
		`create table if not exists login_failures (ip text not null, happened_at text not null);`,
		`create table if not exists shares (
 id text primary key, title text not null, visibility text not null, private_key_hash text, encrypted integer not null,
 cipher_meta text, zip_manifest text, size integer not null, blob_path text not null, blob_sha256 text not null,
 uploader_ip text not null, expires_at text, created_at text not null, purged_at text
);`,
		`create index if not exists idx_shares_visibility on shares(visibility);`,
		`create index if not exists idx_shares_private_key on shares(private_key_hash);`,
		`create index if not exists idx_shares_expires on shares(expires_at);`,
		`create index if not exists idx_sessions_expires on sessions(expires_at);`,
		`create table if not exists audit_events (id integer primary key autoincrement, actor text not null, ip text not null, action text not null, target text, meta text, created_at text not null);`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
