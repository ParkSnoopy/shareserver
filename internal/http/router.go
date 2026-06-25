package httpx

import (
	"encoding/json"
	"fmt"
	"github.com/go-chi/chi/v5"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"shareserver/internal/app"
	"shareserver/internal/auth"
	"shareserver/internal/share"
	"shareserver/internal/upload"
	"time"
)

// New wires middleware, routes, templates, share store, and upload policy.
func New(a *app.App) http.Handler {
	store := share.NewStore(a.DB)
	h := &Handler{A: a, Store: store, Upload: &upload.Uploader{
		Cfg: upload.Config{
			BlobDir:         a.C.BlobDir,
			MaxUploadBytes:  a.C.MaxUploadBytes,
			StorageCapBytes: a.C.StorageCapBytes,
			AppSecret:       a.C.AppSecret,
		},
		Store: store,
		DB:    a.DB,
	}}
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
			return t.In(a.C.TZ).Format("2006-01-02 15:04:05")
		},
	}
	a.T = template.Must(template.New("").Funcs(funcs).ParseGlob(templateGlob()))
	r := chi.NewRouter()
	r.Use(h.security)
	r.Use(h.withSession)
	r.Use(h.csrf)
	r.Get("/download-sw.js", h.downloadServiceWorker)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	r.Get("/", h.home)
	r.Post("/", h.home)
	r.Get("/s/", h.archivePage)
	r.Get("/upload", h.uploadPage)
	r.Post("/upload", h.uploadPost)
	r.Get("/s/{id}", h.sharePage)
	r.Get("/blob/{id}", h.blob)
	r.Get("/admin/login", h.adminLoginPage)
	r.Post("/admin/login", h.adminLoginPost)
	r.Post("/admin/logout", h.adminLogout)
	r.Group(func(r chi.Router) {
		r.Use(h.requireAdmin)
		r.Get("/admin", h.adminDashboard)
		r.Get("/admin/shares", h.adminShares)
		r.Post("/admin/storage/cleanup", h.adminStorageCleanup)
		r.Post("/admin/shares/{id}/delete", h.adminDelete)
	})
	r.NotFound(h.notFoundPage)
	return r
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := h.A.T.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

// notFoundPage renders the friendly 404 page with the home redirect countdown.
func (h *Handler) notFoundPage(w http.ResponseWriter, r *http.Request) {
	h.renderStatus(w, r, http.StatusNotFound, "error.html", map[string]any{
		"Title":           "404",
		"StatusCode":      http.StatusNotFound,
		"Message":         "not found.",
		"RedirectSeconds": 5,
	})
}

// downloadServiceWorker serves the no-store worker used for Android-safe filenames.
func (h *Handler) downloadServiceWorker(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Service-Worker-Allowed", "/")
	http.ServeFile(w, r, "web/static/js/download-sw.js")
}

// home renders public archives or private-key lookup results.
func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	key := ""
	if r.Method == http.MethodPost {
		key = r.FormValue("key")
	}
	keyHash := ""
	if key != "" {
		keyHash = h.privateHash(key)
	}
	h.renderArchive(w, r, share.Share{}, false, "active", keyHash)
}

// archivesForKey selects the public list or private-key matches for the archive sidebar.
func (h *Handler) archivesForKey(keyHash string) ([]share.Share, bool) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if keyHash != "" {
		return h.Store.ListByKey(now, keyHash), true
	}
	return h.Store.ListPublic(now), false
}

// archivePage renders the unselected archive browser.
func (h *Handler) archivePage(w http.ResponseWriter, r *http.Request) {
	h.renderArchive(w, r, share.Share{}, false, "active", "")
}

// uploadPage renders the browser-side zip/encrypt upload form.
func (h *Handler) uploadPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "upload.html", map[string]any{"Max": h.A.C.MaxUploadBytes})
}

// sharePage renders one share, preserving expired status and hiding purged shares.
func (h *Handler) sharePage(w http.ResponseWriter, r *http.Request) {
	s, ok := h.getShare(chi.URLParam(r, "id"))
	if !ok {
		h.notFoundPage(w, r)
		return
	}
	st := share.Status(s, time.Now().UTC())
	if st == "purged" {
		h.notFoundPage(w, r)
		return
	}
	h.renderArchive(w, r, s, true, st, "")
}

// renderArchive feeds the shared archive/detail template for home, lookup, and share pages.
func (h *Handler) renderArchive(w http.ResponseWriter, r *http.Request, selected share.Share, hasSelected bool, status, keyHash string) {
	archives, privateMode := h.archivesForKey(keyHash)
	h.render(w, r, "share.html", map[string]any{
		"Share":       selected,
		"Selected":    hasSelected,
		"Status":      status,
		"Expired":     hasSelected && status == "expired",
		"Archives":    archives,
		"PrivateMode": privateMode,
	})
}

// blob streams the opaque stored archive only while the share is active.
func (h *Handler) blob(w http.ResponseWriter, r *http.Request) {
	s, ok := h.getShare(chi.URLParam(r, "id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if share.Status(s, time.Now().UTC()) != "active" {
		http.Error(w, "expired", 410)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+s.ID+`.blob"`)
	http.ServeFile(w, r, s.BlobPath)
}

// getShare validates UUID input before touching share storage.
func (h *Handler) getShare(id string) (share.Share, bool) {
	if !validUUID(id) {
		return share.Share{}, false
	}
	return h.Store.Get(id)
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validUUID accepts canonical UUID route parameters only.
func validUUID(id string) bool { return uuidRE.MatchString(id) }

// privateHash scopes private-key lookup hashes to the app secret.
func (h *Handler) privateHash(k string) string { return auth.HMACKey(h.A.C.AppSecret, k) }

// jsonResp writes small JSON API responses for browser upload code.
func jsonResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// human formats byte counts for terminal-styled admin pages.
func human(n int64) string { return fmt.Sprintf("%.1f MiB", float64(n)/1024/1024) }

// templateGlob finds templates from repo root or package test working directories.
func templateGlob() string {
	dir, err := os.Getwd()
	if err != nil {
		return "web/templates/*.html"
	}
	for {
		pattern := filepath.Join(dir, "web", "templates", "*.html")
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			return pattern
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "web/templates/*.html"
		}
		dir = parent
	}
}

// purgeOne removes a cleaned blob path as the shared delete primitive.
func purgeOne(path string) error { return os.Remove(filepath.Clean(path)) }
