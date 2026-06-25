package app

import (
	"html/template"
	"shareserver/internal/config"
	"shareserver/internal/ent"
)

// App carries process-wide dependencies shared by HTTP handlers.
type App struct {
	C  config.Config
	DB *ent.Client
	T  *template.Template
}
