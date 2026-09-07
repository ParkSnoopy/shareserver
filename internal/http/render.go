package httpx

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"shareserver/internal/share"
	"strings"
	"time"
)

// human formats byte counts for terminal-styled admin pages.
func human(n int64) string { return fmt.Sprintf("%.1f MiB", float64(n)/1024/1024) }

// setupTemplates builds the template tree with format helpers and stores it on
// the app. Called once during router construction; all render calls share the
// parsed tree.
func setupTemplates(tz *time.Location) *template.Template {
	loadCatalogs()
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

type pageContext struct {
	Title    string
	CSRF     string
	Admin    bool
	Language string
}

type errorPage struct {
	pageContext
	StatusCode      int
	Message         string
	MessageKey      string
	RedirectSeconds int
}

type uploadPageData struct {
	pageContext
	Max int64
}

type archivePageData struct {
	pageContext
	Share       share.Share
	Selected    bool
	Status      string
	Expired     bool
	Archives    []share.Share
	PrivateMode bool
}

type adminDashboardPage struct {
	pageContext
	Used, Cap               int64
	Active, Expired, Purged int
}

type adminSharesPage struct {
	pageContext
	Shares       []share.Share
	DeleteResult string
	Removed      string
	Failed       string
}

func (h *Handler) pageContext(r *http.Request, title string) pageContext {
	session := CurrentSession(r)
	language := session.Language
	if language == "" {
		language = "en"
	}
	return pageContext{Title: title, CSRF: session.CSRF, Admin: session.AdminID > 0, Language: language}
}

func (h *Handler) renderTemplate(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := h.A.T.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (h *Handler) renderErrorPage(w http.ResponseWriter, r *http.Request, status int, message, messageKey string, redirectSeconds int) {
	context := h.pageContext(r, fmt.Sprint(status))
	if messageKey != "" {
		message = context.T(messageKey)
	}
	h.renderTemplate(w, status, "error.html", errorPage{
		pageContext: context,
		StatusCode:  status, Message: message, MessageKey: messageKey, RedirectSeconds: redirectSeconds,
	})
}

func (h *Handler) renderUploadPage(w http.ResponseWriter, r *http.Request) {
	context := h.pageContext(r, "")
	context.Title = strings.TrimPrefix(context.T("upload.title"), "# ")
	h.renderTemplate(w, http.StatusOK, "upload.html", uploadPageData{
		pageContext: context,
		Max:         h.A.C.MaxUploadBytes,
	})
}

func (h *Handler) renderArchivePage(w http.ResponseWriter, r *http.Request, data archivePageData) {
	data.pageContext = h.pageContext(r, data.Share.Title)
	if data.pageContext.Title == "" {
		data.pageContext.Title = strings.TrimPrefix(data.pageContext.T("share.sharesTitle"), "# ")
	}
	h.renderTemplate(w, http.StatusOK, "share.html", data)
}

func (h *Handler) renderAdminLoginPage(w http.ResponseWriter, r *http.Request) {
	context := h.pageContext(r, "")
	context.Title = strings.TrimPrefix(context.T("admin.loginTitle"), "# ")
	h.renderTemplate(w, http.StatusOK, "admin_login.html", context)
}

func (h *Handler) renderAdminDashboardPage(w http.ResponseWriter, r *http.Request, data adminDashboardPage) {
	data.pageContext = h.pageContext(r, "Admin")
	data.pageContext.Title = strings.TrimPrefix(data.pageContext.T("admin.title"), "# ")
	h.renderTemplate(w, http.StatusOK, "admin_dashboard.html", data)
}

func (h *Handler) renderAdminSharesPage(w http.ResponseWriter, r *http.Request, data adminSharesPage) {
	data.pageContext = h.pageContext(r, "Shares")
	data.pageContext.Title = strings.TrimPrefix(data.pageContext.T("admin.sharesTitle"), "# ")
	h.renderTemplate(w, http.StatusOK, "admin_shares.html", data)
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
