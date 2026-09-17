package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
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
	maxPrivateKeyBytes       = 64 << 10
	maxUploadFieldBytes      = (64 << 10) + 1
)

var errUploadFieldTooLarge = errors.New("upload field too large")

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
	fields, file, _, err := uploadParts(r)
	if err != nil {
		if errors.Is(err, errUploadFieldTooLarge) {
			http.Error(w, "metadata too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad upload", 400)
		return
	}
	if file == nil {
		http.Error(w, "missing blob", 400)
		return
	}
	defer file.Close()
	res, err := h.Upload.Do(upload.Request{
		Title:                fields["title"],
		Visibility:           fields["visibility"],
		PrivateKey:           fields["private_key"],
		DownloadPasswordHash: fields["password_hash"],
		CipherMeta:           fields["cipher_meta"],
		ZipManifest:          fields["zip_manifest"],
		EncryptedFlag:        fields["encrypted"],
		ExpiryHours:          fields["expiry_hours"],
		Reader:               file,
		UploaderIP:           ip,
		Admin:                admin,
	})
	if err != nil {
		switch {
		case errors.Is(err, upload.ErrTooLarge):
			http.Error(w, "upload too large after zip/encrypt", http.StatusRequestEntityTooLarge)
		case errors.Is(err, upload.ErrPrivateKeyRequired):
			http.Error(w, "private key required", 400)
		case errors.Is(err, upload.ErrPasswordHashRequired):
			http.Error(w, "download password hash required", 400)
		case errors.Is(err, upload.ErrPasswordHashInvalid):
			http.Error(w, "invalid password hash", 400)
		case errors.Is(err, upload.ErrEncryptionRequired):
			http.Error(w, "invalid encryption mode", 400)
		case errors.Is(err, upload.ErrMetadataTooLarge):
			http.Error(w, "metadata too large", http.StatusRequestEntityTooLarge)
		case errors.Is(err, upload.ErrInvalidBody):
			http.Error(w, "bad upload", http.StatusBadRequest)
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

type privateArchiveResponse struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	URL         string          `json:"url"`
	DownloadURL string          `json:"download_url"`
	Size        int64           `json:"size"`
	ExpiresAt   string          `json:"expires_at"`
	CreatedAt   string          `json:"created_at"`
	CipherMeta  json.RawMessage `json:"cipher_meta"`
	Encryption  string          `json:"encryption"`
}

// apiPrivateListPost returns active private archives matching one body-only lookup key.
func (h *Handler) apiPrivateListPost(w http.ResponseWriter, r *http.Request) {
	if !h.secureRequest(r) {
		http.Error(w, "HTTPS required", http.StatusUpgradeRequired)
		return
	}
	privateKey, err := requestBodyValue(r, "private_key", maxPrivateKeyBytes)
	if err != nil {
		http.Error(w, "invalid private key request", http.StatusBadRequest)
		return
	}
	h.ReconcileBlobStore()
	matches := h.Store.ListByKey(share.ActiveAt(requestTime(r)), h.privateHash(privateKey))
	archives := make([]privateArchiveResponse, 0, len(matches))
	for _, match := range matches {
		cipherMeta := json.RawMessage(match.CipherMeta)
		if !json.Valid(cipherMeta) {
			cipherMeta = json.RawMessage(`{}`)
		}
		expiresAt := ""
		if match.ExpiresAt.Valid {
			expiresAt = match.ExpiresAt.String
		}
		archives = append(archives, privateArchiveResponse{
			ID: match.ID, Title: match.Title, URL: "/s/" + match.ID,
			DownloadURL: "/api/v0/download/" + match.ID,
			ExpiresAt:   expiresAt, CreatedAt: match.CreatedAt, Encryption: "client",
			Size: match.Size, CipherMeta: cipherMeta,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"archives": archives})
}

// uploadParts reads metadata in memory and returns the blob as a live stream.
// The blob must follow its metadata, as emitted by the web client and curl
// example, so metadata is validated before encrypted payload bytes are stored.
func uploadParts(r *http.Request) (map[string]string, io.ReadCloser, string, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, nil, "", err
	}
	fields := map[string]string{}
	allowed := map[string]bool{
		"csrf": true, "title": true, "visibility": true, "private_key": true,
		"password_hash": true, "cipher_meta": true, "zip_manifest": true,
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
			return fields, &uploadPart{Part: part, reader: reader}, part.FileName(), nil
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
			return nil, nil, "", errUploadFieldTooLarge
		}
		fields[name] = string(value)
	}
}

type uploadPart struct {
	*multipart.Part
	reader  *multipart.Reader
	checked bool
}

// Read maps HTTP body-limit failures and requires the blob to be the final part.
func (p *uploadPart) Read(buffer []byte) (int, error) {
	n, err := p.Part.Read(buffer)
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return n, storage.ErrTooLarge
	}
	if errors.Is(err, io.EOF) && !p.checked {
		p.checked = true
		next, nextErr := p.reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			return n, io.EOF
		}
		if next != nil {
			next.Close()
		}
		return n, upload.ErrInvalidBody
	}
	return n, err
}

// apiDownloadPost verifies the separately stored password hash before exposing
// any encrypted payload bytes. It never decrypts the client ciphertext.
func (h *Handler) apiDownloadPost(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	wait := func() {
		if remaining := downloadResponseDelay - time.Since(started); remaining > 0 {
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
		h.Downloads = newDownloadProtection(h.A.DB)
	}
	if until, banned := h.Downloads.bannedUntil(r.Context(), ip, started); banned {
		wait()
		w.Header().Set("Retry-After", retryAfterSeconds(until, started))
		http.Error(w, "too many download attempts", http.StatusTooManyRequests)
		return
	}
	target := chi.URLParam(r, "id")
	deny := func() {
		until, banned, err := h.Downloads.recordFailure(r.Context(), ip, started)
		wait()
		if err != nil {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "too many download attempts", http.StatusTooManyRequests)
			return
		}
		if banned {
			w.Header().Set("Retry-After", retryAfterSeconds(until, started))
			http.Error(w, "too many download attempts", http.StatusTooManyRequests)
			return
		}
		http.Error(w, "download denied", http.StatusUnauthorized)
	}
	passwordHash, err := downloadPasswordHash(r)
	if err != nil {
		deny()
		return
	}
	s, ok := h.getShare(target)
	passwordMatches := ok && auth.CheckPasswordHash(s.DownloadPasswordHash, passwordHash)
	if !ok || !s.Encrypted || s.DownloadPasswordHash == "" || !passwordMatches {
		deny()
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

// downloadPasswordHash accepts only the canonical SHA-256 authorization token;
// plaintext archive passwords never cross the download API boundary.
func downloadPasswordHash(r *http.Request) (string, error) {
	return requestBodyValue(r, "password_hash", maxDownloadPasswordBytes)
}

// requestBodyValue accepts exactly one named string in JSON or form data.
func requestBodyValue(r *http.Request, field string, maximum int64) (string, error) {
	if r.URL.RawQuery != "" {
		return "", errors.New("invalid body value request")
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return "", errors.New("invalid body value request")
	}
	if contentType == "application/json" {
		encodedLimit := maximum*6 + 256
		body, err := io.ReadAll(io.LimitReader(r.Body, encodedLimit+1))
		if err != nil || int64(len(body)) > encodedLimit {
			return "", errors.New("invalid body value request")
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		start, err := dec.Token()
		if err != nil || start != json.Delim('{') || !dec.More() {
			return "", errors.New("invalid body value request")
		}
		name, err := dec.Token()
		if err != nil || name != field {
			return "", errors.New("invalid body value request")
		}
		var value string
		if err := dec.Decode(&value); err != nil || value == "" || int64(len(value)) > maximum || dec.More() {
			return "", errors.New("invalid body value request")
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return "", errors.New("invalid body value request")
		}
		if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return "", errors.New("invalid body value request")
		}
		return value, nil
	}
	if contentType != "application/x-www-form-urlencoded" {
		return "", errors.New("invalid body value request")
	}
	formBody, err := io.ReadAll(io.LimitReader(r.Body, maximum*3+257))
	if err != nil || int64(len(formBody)) > maximum*3+256 {
		return "", errors.New("invalid body value request")
	}
	form, err := url.ParseQuery(string(formBody))
	if err != nil {
		return "", errors.New("invalid body value request")
	}
	values := form[field]
	if len(form) != 1 || len(values) != 1 || values[0] == "" || int64(len(values[0])) > maximum {
		return "", errors.New("invalid body value request")
	}
	return values[0], nil
}

// adminLoginPage renders the admin sign-in form.
func (h *Handler) adminLoginPage(w http.ResponseWriter, r *http.Request) {
	h.renderAdminLoginPage(w, r)
}

// adminLoginPost delegates credential decisions to auth and rotates admin session state on success.
func (h *Handler) adminLoginPost(w http.ResponseWriter, r *http.Request) {
	ip := h.clientIP(r)
	if r.URL.RawQuery != "" || r.ParseForm() != nil || len(r.PostForm) != 3 || len(r.PostForm["username"]) != 1 || len(r.PostForm["password_hash"]) != 1 || len(r.PostForm["csrf"]) != 1 {
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	user, passwordHash := r.PostForm["username"][0], r.PostForm["password_hash"][0]
	result := auth.AdminLogin(r.Context(), h.A.DB, ip, user, passwordHash, time.Now())
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
