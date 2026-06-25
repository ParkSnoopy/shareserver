package db

import (
	"context"
	stdsql "database/sql"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/mattn/go-sqlite3"
	"shareserver/internal/ent"
)

// Open creates the SQLite-backed Ent client and runs deterministic migrations.
func Open(path string) (*ent.Client, error) {
	db, err := stdsql.Open("sqlite3", path+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	if err := client.Schema.Create(context.Background()); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// Now returns durable UTC timestamps in the database format.
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
