package app

import (
	"html/template"
	"shareserver/internal/config"
	"shareserver/internal/ent"
	"shareserver/internal/storage"
)

// App carries process-wide dependencies shared by HTTP handlers.
type App struct {
	C         config.Config
	DB        *ent.Client
	T         *template.Template
	Integrity *storage.Integrity
}
