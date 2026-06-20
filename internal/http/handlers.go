package httpx

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"net/http"
	"shareserver/internal/audit"
	"shareserver/internal/auth"
	"shareserver/internal/storage"
	"shareserver/internal/upload"
	"time"
)

func (h *Handler) uploadPost(w http.ResponseWriter, r *http.Request) {
	ip := h.clientIP(r)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			http.Error(w, "upload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad upload", 400)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, _, err := r.FormFile("blob")
	if err != nil {
		http.Error(w, "missing blob", 400)
		return
	}
	defer file.Close()
	res, err := h.Upload.Do(upload.Request{
		Title:         r.FormValue("title"),
		Visibility:    r.FormValue("visibility"),
		PrivateKey:    r.FormValue("private_key"),
		CipherMeta:    r.FormValue("cipher_meta"),
		ZipManifest:   r.FormValue("zip_manifest"),
		EncryptedFlag: r.FormValue("encrypted"),
		ExpiryHours:   r.FormValue("expiry_hours"),
		Reader:        file,
		UploaderIP:    ip,
	})
	if err != nil {
		switch {
		case errors.Is(err, upload.ErrTooLarge):
			http.Error(w, "upload too large after zip/encrypt", http.StatusRequestEntityTooLarge)
		case errors.Is(err, upload.ErrPrivateKeyRequired):
			http.Error(w, "private key required", 400)
		case errors.Is(err, upload.ErrMetadataTooLarge):
			http.Error(w, "metadata too large", http.StatusRequestEntityTooLarge)
		case errors.Is(err, upload.ErrCap):
			http.Error(w, "server couldn't keep this right now. try again later.", http.StatusInsufficientStorage)
		default:
			http.Error(w, "server couldn't keep this right now. try again later.", 500)
		}
		return
	}
	jsonResp(w, map[string]any{"ok": true, "id": res.ID, "url": res.URL})
}

func (h *Handler) adminLoginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "admin_login.html", nil)
}
func (h *Handler) adminLoginPost(w http.ResponseWriter, r *http.Request) {
	ip := h.clientIP(r)
	now := time.Now()
	if auth.IsBanned(h.A.DB, ip, now) {
		audit.Log(h.A.DB, "public", ip, "login_banned", "", "")
		http.Error(w, "try again later", 429)
		return
	}
	user, pass := r.FormValue("username"), r.FormValue("password")
	var id int64
	var hash string
	err := h.A.DB.QueryRow(`select id,password_hash from admins where username=?`, user).Scan(&id, &hash)
	if err != nil || !auth.CheckPassword(hash, pass) {
		banned, until := auth.RecordLoginFailure(h.A.DB, ip, now)
		meta := ""
		if banned {
			meta = "banned_until=" + until.Format(time.RFC3339Nano)
		}
		audit.Log(h.A.DB, "public", ip, "login_fail", user, meta)
		http.Error(w, "login failed", 401)
		return
	}
	auth.ResetFailures(h.A.DB, ip)
	h.loginSession(w, r, CurrentSession(r).ID, id)
	audit.Log(h.A.DB, "admin", ip, "login", user, "")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
func (h *Handler) adminLogout(w http.ResponseWriter, r *http.Request) {
	h.logoutSession(CurrentSession(r).ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (h *Handler) adminDashboard(w http.ResponseWriter, r *http.Request) {
	used := storage.UsedBytes(h.A.C.BlobDir)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	h.render(w, r, "admin_dashboard.html", map[string]any{
		"Used": used, "Cap": h.A.C.StorageCapBytes,
		"Active": h.Store.CountActive(now), "Expired": h.Store.CountExpired(now), "Purged": h.Store.CountPurged(),
	})
}
func (h *Handler) adminShares(w http.ResponseWriter, r *http.Request) {
	list := h.Store.ListAll()
	h.render(w, r, "admin_shares.html", map[string]any{"Shares": list, "Now": time.Now().UTC()})
}
func (h *Handler) adminDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s, ok := h.getShare(id); ok {
		purgeOne(s.BlobPath)
		_ = h.Store.Delete(id)
		audit.Log(h.A.DB, "admin", h.clientIP(r), "delete", id, "removed blob and metadata")
	}
	http.Redirect(w, r, "/admin/shares", 303)
}
