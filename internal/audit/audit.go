package audit

import (
	"context"

	"shareserver/internal/db"
	"shareserver/internal/ent"
)

// Log records a safe audit event and intentionally ignores logging failures.
func Log(client *ent.Client, actor, ip, action, target, meta string) {
	_, _ = client.AuditEvent.Create().
		SetActor(actor).
		SetIP(ip).
		SetAction(action).
		SetTarget(target).
		SetMeta(meta).
		SetCreatedAt(db.Now()).
		Save(context.Background())
}
