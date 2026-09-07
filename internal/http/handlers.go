package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"io"
	"mime/multipart"
	"net/http"
	"shareserver/internal/audit"
	"shareserver/internal/auth"
	"shareserver/internal/share"
	"shareserver/internal/storage"
	"shareserver/internal/upload"
	"strings"
	"time"
)

const (
	maxDownloadPasswordBytes = 4 << 10
	maxUploadFieldBytes      = (64 << 10) + 1
)

// apiUploadPost accepts a client-encrypted archive and delegates storage policy to upload.
func (h *Handler) apiUploadPost(w http.ResponseWriter, r *http.Request) {
	if !h.secureRequest(r) {
		http.Error(w, "HTTPS required", http.StatusUpgradeRequired)
		return
	}
	ip := h.clientIP(r)
	// API clients need no browser session. A browser admin may opt into its
	// longer expiry allowance only by presenting its session-bound CSRF token.
	tok := r.Header.Get("X-CSRF-Token")
	session := CurrentSession(r)
	admin := false
	if tok != "" {
		if session.CSRF == "" || !sameToken(tok, session.CSRF) {
			http.Error(w, "csrf rejected", http.StatusForbidden)
			return
		}
		admin = session.AdminID > 0
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		http.Error(w, "multipart upload required", http.StatusUnsupportedMediaType)
		return
	}
	fields, file, filename, err := uploadParts(r)
	if err != nil {
		http.Error(w, "bad upload", 400)
		return
	}
	if file == nil {
		http.Error(w, "missing blob", 400)
		return
	}
	defer file.Close()
	res, err := h.Upload.Do(upload.Request{
		Title:            fields["title"],
		Visibility:       fields["visibility"],
		PrivateKey:       fields["private_key"],
		DownloadPassword: fields["password"],
		CipherMeta:       fields["cipher_meta"],
		ZipManifest:      fields["zip_manifest"],
		EncryptedFlag:    fields["encrypted"],
		ExpiryHours:      fields["expiry_hours"],
		Filename:         filename,
		Reader:           file,
		UploaderIP:       ip,
		Admin:            admin,
	})
	if err != nil {
		switch {
		case errors.Is(err, upload.ErrTooLarge):
			http.Error(w, "upload too large after zip/encrypt", http.StatusRequestEntityTooLarge)
		case errors.Is(err, upload.ErrPrivateKeyRequired):
			http.Error(w, "private key required", 400)
		case errors.Is(err, upload.ErrPasswordRequired):
			http.Error(w, "download password required", 400)
		case errors.Is(err, upload.ErrEncryptionRequired):
			http.Error(w, "invalid encryption mode", 400)
		case errors.Is(err, upload.ErrMetadataTooLarge):
			http.Error(w, "metadata too large", http.StatusRequestEntityTooLarge)
		case errors.Is(err, upload.ErrCap):
			http.Error(w, "server couldn't keep this right now. try again later.", http.StatusInsufficientStorage)
		default:
			http.Error(w, "server couldn't keep this right now. try again later.", 500)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": res.ID, "url": res.URL, "download_url": res.DownloadURL,
		"size": res.Size, "expires_at": res.ExpiresAt,
		"cipher_meta": json.RawMessage(res.CipherMeta), "encryption": res.Encryption,
	})
}

// uploadParts reads metadata in memory and returns the blob as a live stream.
// The blob must follow its metadata, as emitted by the web client and curl
// example, so plain uploads never spill unencrypted bytes into multipart temp files.
func uploadParts(r *http.Request) (map[string]string, io.ReadCloser, string, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, nil, "", err
	}
	fields := map[string]string{}
	allowed := map[string]bool{
		"csrf": true, "title": true, "visibility": true, "private_key": true,
		"password": true, "cipher_meta": true, "zip_manifest": true,
		"encrypted": true, "expiry_hours": true,
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return fields, nil, "", nil
		}
		if err != nil {
			return nil, nil, "", err
		}
		if part.FormName() == "blob" {
			if part.FileName() == "" {
				part.Close()
				return nil, nil, "", errors.New("blob filename required")
			}
			return fields, &uploadPart{Part: part}, part.FileName(), nil
		}
		name := part.FormName()
		if part.FileName() != "" || !allowed[name] {
			part.Close()
			return nil, nil, "", errors.New("unexpected upload field")
		}
		if _, duplicate := fields[name]; duplicate {
			part.Close()
			return nil, nil, "", errors.New("duplicate upload field")
		}
		value, err := io.ReadAll(io.LimitReader(part, maxUploadFieldBytes))
		part.Close()
		if err != nil {
			return nil, nil, "", err
		}
		if len(value) == maxUploadFieldBytes {
			return nil, nil, "", errors.New("upload field too large")
		}
		fields[name] = string(value)
	}
}

type uploadPart struct {
	*multipart.Part
}

// Read maps HTTP body-limit failures into the storage limit error understood by upload policy.
func (p *uploadPart) Read(buffer []byte) (int, error) {
	n, err := p.Part.Read(buffer)
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return n, storage.ErrTooLarge
	}
	return n, err
}

// apiDownloadPost verifies the separately stored password hash before exposing
// any encrypted payload bytes. It never decrypts the client ciphertext.
func (h *Handler) apiDownloadPost(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	wait := func() {
		if remaining := time.Second - time.Since(started); remaining > 0 {
			time.Sleep(remaining)
		}
	}
	if !h.secureRequest(r) {
		wait()
		http.Error(w, "HTTPS required", http.StatusUpgradeRequired)
		return
	}
	ip := h.clientIP(r)
	if h.Downloads == nil {
		h.Downloads = newDownloadRateLimiter(5, time.Minute)
	}
	if !h.Downloads.Allow(ip, started) {
		wait()
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many download attempts", http.StatusTooManyRequests)
		return
	}
	password, err := downloadPassword(r)
	if err != nil {
		wait()
		http.Error(w, "download denied", http.StatusUnauthorized)
		return
	}
	s, ok := h.getShare(chi.URLParam(r, "id"))
	if !ok || !s.Encrypted || s.DownloadPasswordHash == "" || !auth.CheckDownloadPassword(s.DownloadPasswordHash, password) {
		wait()
		http.Error(w, "download denied", http.StatusUnauthorized)
		return
	}
	if !share.ActiveAt(requestTime(r)).IsActive(s) {
		wait()
		http.Error(w, "expired", http.StatusGone)
		return
	}
	wait()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+s.ID+`.payload"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, s.BlobPath)
}

func downloadPassword(r *http.Request) (string, error) {
	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "application/json") {
		var body struct {
			Password string `json:"password"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, maxDownloadPasswordBytes+256))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil || body.Password == "" || len(body.Password) > maxDownloadPasswordBytes {
			return "", errors.New("invalid password request")
		}
		if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return "", errors.New("invalid password request")
		}
		return body.Password, nil
	}
	if err := r.ParseForm(); err != nil || r.FormValue("password") == "" || len(r.FormValue("password")) > maxDownloadPasswordBytes {
		return "", errors.New("invalid password request")
	}
	return r.FormValue("password"), nil
}

// adminLoginPage renders the admin sign-in form.
func (h *Handler) adminLoginPage(w http.ResponseWriter, r *http.Request) {
	h.renderAdminLoginPage(w, r)
}

// adminLoginPost delegates credential decisions to auth and rotates admin session state on success.
func (h *Handler) adminLoginPost(w http.ResponseWriter, r *http.Request) {
	ip := h.clientIP(r)
	user, pass := r.FormValue("username"), r.FormValue("password")
	result := auth.AdminLogin(r.Context(), h.A.DB, ip, user, pass, time.Now())
	switch result.Status {
	case auth.AdminLoginBanned:
		audit.Log(h.A.DB, "public", ip, "login_banned", "", "")
		http.Error(w, "try again later", 429)
	case auth.AdminLoginFailed:
		meta := ""
		if !result.BannedUntil.IsZero() {
			meta = "banned_until=" + result.BannedUntil.Format(time.RFC3339Nano)
		}
		audit.Log(h.A.DB, "public", ip, "login_fail", user, meta)
		http.Error(w, "login failed", 401)
	case auth.AdminLoginSuccess:
		h.loginSession(w, r, CurrentSession(r).ID, int64(result.AdminID))
		audit.Log(h.A.DB, "admin", ip, "login", user, "")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	default:
		audit.Log(h.A.DB, "public", ip, "login_fail", user, "")
		http.Error(w, "login failed", 401)
	}
}

// adminLogout removes the current admin session and returns to the public home page.
func (h *Handler) adminLogout(w http.ResponseWriter, r *http.Request) {
	h.logoutSession(CurrentSession(r).ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// adminDashboard reconciles storage before showing synchronized counters.
func (h *Handler) adminDashboard(w http.ResponseWriter, r *http.Request) {
	h.ReconcileBlobStore()
	used := storage.UsedBytes(h.A.C.BlobDir)
	active := share.ActiveAt(requestTime(r))
	h.renderAdminDashboardPage(w, r, adminDashboardPage{
		Used: used, Cap: h.A.C.StorageCapBytes,
		Active: h.Store.CountActive(active), Expired: h.Store.CountExpired(active), Purged: h.Store.CountPurged(),
	})
}

// adminShares reconciles storage before listing Shares for inspection and deletion.
func (h *Handler) adminShares(w http.ResponseWriter, r *http.Request) {
	h.ReconcileBlobStore()
	list := h.Store.ListAll()
	h.renderAdminSharesPage(w, r, adminSharesPage{
		Shares: list, DeleteResult: r.URL.Query().Get("delete"),
		Removed: r.URL.Query().Get("removed"), Failed: r.URL.Query().Get("failed"),
	})
}

// adminBulkDelete removes each selected Share as one blob/database pair.
func (h *Handler) adminBulkDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad selection", http.StatusBadRequest)
		return
	}
	removed, failed := 0, 0
	seen := map[string]struct{}{}
	for _, id := range r.Form["ids"] {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		if len(seen) > 300 || !validUUID(id) {
			failed++
			continue
		}
		sh, ok := h.Store.Get(id)
		if !ok {
			failed++
			continue
		}
		if err := h.integrity().Remove(sh); err != nil {
			audit.Log(h.A.DB, "admin", h.clientIP(r), "delete_failed", id, err.Error())
			failed++
			continue
		}
		audit.Log(h.A.DB, "admin", h.clientIP(r), "delete", id, "removed blob and metadata")
		removed++
	}

	result := "done"
	if removed == 0 && failed == 0 {
		result = "none"
	} else if failed > 0 {
		result = "failed"
	}
	h.ReconcileBlobStore()
	http.Redirect(w, r, fmt.Sprintf("/admin/shares?delete=%s&removed=%d&failed=%d", result, removed, failed), http.StatusSeeOther)
}
