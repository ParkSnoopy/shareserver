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
	a.T = template.Must(template.New("").Funcs(funcs).ParseGlob("web/templates/*.html"))
	r := chi.NewRouter()
	r.Use(h.security)
	r.Use(h.withSession)
	r.Use(h.csrf)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	r.Get("/", h.home)
	r.Post("/lookup", h.lookup)
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
		r.Post("/admin/shares/{id}/delete", h.adminDelete)
	})
	return r
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["CSRF"] = CurrentSession(r).CSRF
	data["Admin"] = CurrentSession(r).AdminID > 0
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.A.T.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) { h.renderShares(w, r, "", false) }
func (h *Handler) lookup(w http.ResponseWriter, r *http.Request) {
	h.renderShares(w, r, r.FormValue("key"), true)
}
func (h *Handler) renderShares(w http.ResponseWriter, r *http.Request, key string, keyMode bool) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var list []share.Share
	if key != "" {
		list = h.Store.ListByKey(now, h.privateHash(key))
	} else {
		list = h.Store.ListPublic(now)
	}
	h.render(w, r, "home.html", map[string]any{"Shares": list, "KeyMode": keyMode})
}
func (h *Handler) uploadPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "upload.html", map[string]any{"Max": h.A.C.MaxUploadBytes})
}

func (h *Handler) sharePage(w http.ResponseWriter, r *http.Request) {
	s, ok := h.getShare(chi.URLParam(r, "id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	st := share.Status(s, time.Now().UTC())
	if st == "purged" {
		http.NotFound(w, r)
		return
	}
	h.render(w, r, "share.html", map[string]any{"Share": s, "Status": st, "Expired": st == "expired"})
}

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

func (h *Handler) getShare(id string) (share.Share, bool) {
	if !validUUID(id) {
		return share.Share{}, false
	}
	return h.Store.Get(id)
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validUUID(id string) bool { return uuidRE.MatchString(id) }

func (h *Handler) privateHash(k string) string { return auth.HMACKey(h.A.C.AppSecret, k) }

func jsonResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func human(n int64) string { return fmt.Sprintf("%.1f MiB", float64(n)/1024/1024) }

func purgeOne(path string) { _ = os.Remove(filepath.Clean(path)) }
