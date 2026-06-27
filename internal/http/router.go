package httpx

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-chi/chi/v5"

	"shareserver/internal/app"
	"shareserver/internal/auth"
	"shareserver/internal/share"
	"shareserver/internal/upload"
)

// Handler groups HTTP dependencies; route methods keep policy in owned modules.
type Handler struct {
	A        *app.App
	Store    *share.Store
	Upload   *upload.Uploader
	Sessions SessionLifecycle
}

// New wires middleware, routes, templates, share store, and upload policy.
func New(a *app.App) http.Handler {
	store := share.NewStore(a.DB)
	h := &Handler{
		A:        a,
		Store:    store,
		Sessions: NewSessions(a.DB),
		Upload: &upload.Uploader{
			Cfg: upload.Config{
				BlobDir:         a.C.BlobDir,
				MaxUploadBytes:  a.C.MaxUploadBytes,
				StorageCapBytes: a.C.StorageCapBytes,
				AppSecret:       a.C.AppSecret,
			},
			Store: store,
			DB:    a.DB,
		},
	}
	a.T = setupTemplates(a.C.TZ)
	r := chi.NewRouter()
	r.Use(h.security)
	r.Use(h.withClock)
	r.Use(h.withSession)
	r.Use(h.csrf)
	r.Get("/download-sw.js", h.downloadServiceWorker)
	if a.C.Dev {
		r.Get("/dev/debug.js", h.devDebugScript)
		r.Get("/dev/debug.css", h.devDebugStyle)
	}
	r.Handle("/static/*", noCacheStatic(http.StripPrefix("/static/", http.FileServer(http.Dir(repoFile("web", "static"))))))
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
	http.ServeFile(w, r, repoFile("web", "static", "js", "download-sw.js"))
}

// devDebugScript serves local development-only browser diagnostics.
func (h *Handler) devDebugScript(w http.ResponseWriter, r *http.Request) {
	h.serveDevFile(w, r, "web/dev/debug.js")
}

// devDebugStyle serves local development-only browser diagnostic styles.
func (h *Handler) devDebugStyle(w http.ResponseWriter, r *http.Request) {
	h.serveDevFile(w, r, "web/dev/debug.css")
}

// serveDevFile fails closed so development assets are unreachable in production.
func (h *Handler) serveDevFile(w http.ResponseWriter, r *http.Request, path string) {
	if !h.A.C.Dev {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, repoFile(path))
}

// noCacheStatic wraps the static file server so browsers always revalidate
// (If-Modified-Since) before using a cached asset. Without this, heuristic
// caching can serve a stale progress.js whose API no longer matches the
// freshly-fetched share.js, causing "progress.state is not a function" and
// similar stale-module crashes on devices that visited the page before an
// update. The 304 path still avoids re-sending unchanged bytes.
func noCacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		next.ServeHTTP(w, r)
	})
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
	h.renderArchive(w, r, share.Share{}, false, share.StatusActive, keyHash)
}

// archivesForKey selects the public list or private-key matches for the archive sidebar.
func (h *Handler) archivesForKey(r *http.Request, keyHash string) ([]share.Share, bool) {
	active := share.ActiveAt(requestTime(r))
	if keyHash != "" {
		return h.Store.ListByKey(active, keyHash), true
	}
	return h.Store.ListPublic(active), false
}

// archivePage renders the unselected archive browser.
func (h *Handler) archivePage(w http.ResponseWriter, r *http.Request) {
	h.renderArchive(w, r, share.Share{}, false, share.StatusActive, "")
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
	st := share.ActiveAt(requestTime(r)).Status(s)
	if st == share.StatusPurged {
		h.notFoundPage(w, r)
		return
	}
	h.renderArchive(w, r, s, true, st, "")
}

// renderArchive feeds the shared archive/detail template for home, lookup, and share pages.
func (h *Handler) renderArchive(w http.ResponseWriter, r *http.Request, selected share.Share, hasSelected bool, status, keyHash string) {
	archives, privateMode := h.archivesForKey(r, keyHash)
	h.render(w, r, "share.html", map[string]any{
		"Share":       selected,
		"Selected":    hasSelected,
		"Status":      status,
		"Expired":     hasSelected && status == share.StatusExpired,
		"Archives":    archives,
		"PrivateMode": privateMode,
		"Dev":         h.A.C.Dev,
	})
}

// blob streams the opaque stored archive only while the share is active.
func (h *Handler) blob(w http.ResponseWriter, r *http.Request) {
	s, ok := h.getShare(chi.URLParam(r, "id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if !share.ActiveAt(requestTime(r)).IsActive(s) {
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


// repoFile finds a repository-relative file from repo root or package test directories.
func repoFile(parts ...string) string {
	dir, err := os.Getwd()
	if err != nil {
		return filepath.Join(parts...)
	}
	for {
		elems := append([]string{dir}, parts...)
		path := filepath.Join(elems...)
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join(parts...)
		}
		dir = parent
	}
}
