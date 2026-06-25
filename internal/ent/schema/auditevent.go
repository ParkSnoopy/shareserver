package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// AuditEvent stores administrator and upload audit records.
type AuditEvent struct {
	ent.Schema
}

// Fields of the AuditEvent.
func (AuditEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("actor"),
		field.String("ip"),
		field.String("action"),
		field.String("target"),
		field.String("meta"),
		field.String("created_at"),
	}
}
