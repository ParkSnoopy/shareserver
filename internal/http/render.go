package httpx

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// human formats byte counts for terminal-styled admin pages.
func human(n int64) string { return fmt.Sprintf("%.1f MiB", float64(n)/1024/1024) }

// setupTemplates builds the template tree with format helpers and stores it on
// the app. Called once during router construction; all render calls share the
// parsed tree.
func setupTemplates(tz *time.Location) *template.Template {
	funcs := template.FuncMap{
		"csrf": func() string { return "" },
		"short": func(s string) string {
			if len(s) > 8 {
				return s[:8]
			}
			return s
		},
		"mb": func(n int64) string { return human(n) },
		"fmtTime": func(s string) string {
			t, err := time.Parse(time.RFC3339Nano, s)
			if err != nil {
				return s
			}
			return t.In(tz).Format("2006-01-02 15:04:05")
		},
	}
	dir := templateDir()
	t := template.Must(template.New("").Funcs(funcs).ParseGlob(filepath.Join(dir, "*.html")))
	return template.Must(t.ParseGlob(filepath.Join(dir, "admin", "*.html")))
}

// render writes a normal 200 HTML template response with shared template data.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	h.renderStatus(w, r, http.StatusOK, name, data)
}

// renderStatus writes an HTML template with CSRF/admin context and explicit status.
func (h *Handler) renderStatus(w http.ResponseWriter, r *http.Request, status int, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["CSRF"] = CurrentSession(r).CSRF
	data["Admin"] = CurrentSession(r).AdminID > 0
	data["Dev"] = h.A.C.Dev
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := h.A.T.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

// templateDir finds web/templates from repo root or package test working directories.
func templateDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return filepath.Join("web", "templates")
	}
	for {
		path := filepath.Join(dir, "web", "templates")
		pattern := filepath.Join(path, "*.html")
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			return path
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join("web", "templates")
		}
		dir = parent
	}
}
