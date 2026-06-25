package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// LoginFailureEvent stores recent failed login attempts for rate limiting.
type LoginFailureEvent struct {
	ent.Schema
}

// Fields of the LoginFailureEvent.
func (LoginFailureEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("ip"),
		field.String("happened_at"),
	}
}

// Indexes of the LoginFailureEvent.
func (LoginFailureEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("ip"),
		index.Fields("happened_at"),
	}
}
