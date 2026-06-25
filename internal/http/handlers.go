package httpx

import (
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"net/http"
	"os"
	"shareserver/internal/audit"
	"shareserver/internal/auth"
	entadmin "shareserver/internal/ent/admin"
	"shareserver/internal/storage"
	"shareserver/internal/upload"
	"time"
)

// uploadPost accepts a browser-built archive and delegates validation/storage to upload.
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
		Admin:         CurrentSession(r).AdminID > 0,
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

// adminLoginPage renders the admin sign-in form.
func (h *Handler) adminLoginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "admin_login.html", nil)
}

// adminLoginPost verifies credentials, rate-limits failures, and rotates admin session state.
func (h *Handler) adminLoginPost(w http.ResponseWriter, r *http.Request) {
	ip := h.clientIP(r)
	now := time.Now()
	if auth.IsBanned(h.A.DB, ip, now) {
		audit.Log(h.A.DB, "public", ip, "login_banned", "", "")
		http.Error(w, "try again later", 429)
		return
	}
	user, pass := r.FormValue("username"), r.FormValue("password")
	adminRow, err := h.A.DB.Admin.Query().
		Where(entadmin.UsernameEQ(user)).
		Only(r.Context())
	if err != nil || !auth.CheckPassword(adminRow.PasswordHash, pass) {
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
	h.loginSession(w, r, CurrentSession(r).ID, int64(adminRow.ID))
	audit.Log(h.A.DB, "admin", ip, "login", user, "")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// adminLogout removes the current admin session and returns to the public home page.
func (h *Handler) adminLogout(w http.ResponseWriter, r *http.Request) {
	h.logoutSession(CurrentSession(r).ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// adminDashboard shows storage/share counters and the manual repair action.
func (h *Handler) adminDashboard(w http.ResponseWriter, r *http.Request) {
	used := storage.UsedBytes(h.A.C.BlobDir)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	cleanupDone := r.URL.Query().Get("storage_cleanup") == "done"
	h.render(w, r, "admin_dashboard.html", map[string]any{
		"Used": used, "Cap": h.A.C.StorageCapBytes,
		"Active": h.Store.CountActive(now), "Expired": h.Store.CountExpired(now), "Purged": h.Store.CountPurged(),
		"StorageCleanupDone": cleanupDone,
		"StorageMissingRows": r.URL.Query().Get("missing"),
		"StorageOrphanFiles": r.URL.Query().Get("orphan"),
	})
}

// adminShares lists recent shares for inspection and deletion.
func (h *Handler) adminShares(w http.ResponseWriter, r *http.Request) {
	list := h.Store.ListAll()
	h.render(w, r, "admin_shares.html", map[string]any{"Shares": list, "Now": time.Now().UTC()})
}

// adminDelete removes one share's blob and metadata when the admin confirms deletion.
func (h *Handler) adminDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s, ok := h.getShare(id); ok {
		if err := purgeOne(s.BlobPath); err == nil || os.IsNotExist(err) {
			_ = h.Store.Delete(id)
			audit.Log(h.A.DB, "admin", h.clientIP(r), "delete", id, "removed blob and metadata")
		}
	}
	http.Redirect(w, r, "/admin/shares", 303)
}

// adminStorageCleanup runs storage reconciliation and redirects with repair counts.
func (h *Handler) adminStorageCleanup(w http.ResponseWriter, r *http.Request) {
	result := h.ReconcileBlobStore()
	http.Redirect(w, r, fmt.Sprintf("/admin?storage_cleanup=done&missing=%d&orphan=%d", result.MissingFiles, result.OrphanFiles), http.StatusSeeOther)
}
