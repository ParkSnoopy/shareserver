package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Session stores browser session state.
type Session struct {
	ent.Schema
}

// Fields of the Session.
func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.Int64("admin_id").Optional().Nillable(),
		field.String("csrf"),
		field.String("created_at"),
		field.String("expires_at"),
	}
}

// Indexes of the Session.
func (Session) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("expires_at"),
	}
}
