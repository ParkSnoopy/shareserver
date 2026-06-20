package app

import (
	"database/sql"
	"html/template"
	"shareserver/internal/config"
)

type App struct {
	C  config.Config
	DB *sql.DB
	T  *template.Template
}
