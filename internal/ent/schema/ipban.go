package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// IpBan stores temporary login bans by IP address.
type IpBan struct {
	ent.Schema
}

// Fields of the IpBan.
func (IpBan) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("ip").Immutable(),
		field.String("banned_until"),
	}
}
