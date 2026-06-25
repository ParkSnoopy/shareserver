package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Admin stores administrator credentials.
type Admin struct {
	ent.Schema
}

// Fields of the Admin.
func (Admin) Fields() []ent.Field {
	return []ent.Field{
		field.String("username").Unique(),
		field.String("password_hash"),
		field.String("created_at"),
	}
}
