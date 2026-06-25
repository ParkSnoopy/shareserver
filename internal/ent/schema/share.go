package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Share stores uploaded archive metadata.
type Share struct {
	ent.Schema
}

// Fields of the Share.
func (Share) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("title"),
		field.String("visibility"),
		field.String("private_key_hash").Optional().Nillable(),
		field.Bool("encrypted"),
		field.String("cipher_meta").Optional(),
		field.String("zip_manifest").Optional(),
		field.Int64("size"),
		field.String("blob_path"),
		field.String("blob_sha256"),
		field.String("uploader_ip"),
		field.String("expires_at").Optional().Nillable(),
		field.String("created_at"),
		field.String("purged_at").Optional().Nillable(),
	}
}

// Indexes of the Share.
func (Share) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("visibility"),
		index.Fields("private_key_hash"),
		index.Fields("expires_at"),
	}
}
