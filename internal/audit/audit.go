package audit

import (
	"database/sql"
	"shareserver/internal/db"
)

func Log(d *sql.DB, actor, ip, action, target, meta string) {
	_, _ = d.Exec(`insert into audit_events(actor,ip,action,target,meta,created_at) values(?,?,?,?,?,?)`, actor, ip, action, target, meta, db.Now())
}
