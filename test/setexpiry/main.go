// setexpiry is a test helper that shortens a share's expires_at to now+secs.
// Used by test/smoke.sh to exercise sub-hour expiry over curl (the upload
// endpoint only accepts expiry_hours, min 1h, so we poke the DB directly).
//
// Usage: go run ./test/setexpiry <db> <id> <seconds>
package main

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: setexpiry <db> <id> <seconds>")
		os.Exit(2)
	}
	dbPath, id := os.Args[1], os.Args[2]
	secs, err := parseSecs(os.Args[3])
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad seconds:", err)
		os.Exit(2)
	}
	d, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open db:", err)
		os.Exit(1)
	}
	defer d.Close()
	exp := time.Now().UTC().Add(time.Duration(secs) * time.Second).Format(time.RFC3339Nano)
	res, err := d.Exec(`update shares set expires_at=? where id=?`, exp, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		os.Exit(1)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fmt.Fprintln(os.Stderr, "no row updated for id", id)
		os.Exit(1)
	}
	fmt.Println("ok", id, "expires_at", exp)
}

func parseSecs(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("seconds must be positive")
	}
	return n, nil
}
