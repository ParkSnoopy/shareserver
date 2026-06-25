// Package upload owns the upload policy: validation, cap enforcement, blob
// storage, rollback, metadata insert, and audit. The HTTP handler parses
// multipart only; every rule that can fail lives here.
package upload

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"shareserver/internal/audit"
	"shareserver/internal/auth"
	"shareserver/internal/ent"
	"shareserver/internal/share"
	"shareserver/internal/storage"
	"strconv"
	"sync"
	"time"
)

const (
	maxTitleBytes    = 512
	maxCipherBytes   = 4096
	maxManifestBytes = 64 << 10
)

var capMu sync.Mutex

var (
	ErrTooLarge           = errors.New("upload too large")
	ErrCap                = errors.New("storage cap reached")
	ErrStore              = errors.New("store failed")
	ErrPrivateKeyRequired = errors.New("private key required")
	ErrMetadataTooLarge   = errors.New("metadata too large")
)

// Config is the subset of config the upload policy depends on.
type Config struct {
	BlobDir         string
	MaxUploadBytes  int64
	StorageCapBytes int64
	AppSecret       []byte
}

// Uploader is the deep upload module. One Do call runs the full policy.
type Uploader struct {
	Cfg   Config
	Store *share.Store
	DB    *ent.Client // for audit only
}

// Request is the parsed multipart form plus the blob reader.
type Request struct {
	Title, Visibility, PrivateKey, CipherMeta, ZipManifest string
	EncryptedFlag, ExpiryHours                             string
	Reader                                                 io.Reader
	UploaderIP                                             string
	Admin                                                  bool
}

// Result is what a successful upload yields to the handler.
type Result struct {
	ID, URL string
}

// Do runs the upload policy. On any error no share row is left behind; a blob
// written before a late failure is removed. Validation runs before storage so
// bad requests never write a blob.
func (u *Uploader) Do(req Request) (Result, error) {
	ip := req.UploaderIP

	// Validate first — no blob written for a bad request.
	title := req.Title
	if title == "" {
		title = "untitled share"
	}
	if len(title) > maxTitleBytes || len(req.CipherMeta) > maxCipherBytes || len(req.ZipManifest) > maxManifestBytes {
		return Result{}, ErrMetadataTooLarge
	}

	vis := req.Visibility
	if vis != "private" {
		vis = "public"
	}
	var keyHash string
	if vis == "private" {
		if req.PrivateKey == "" {
			return Result{}, ErrPrivateKeyRequired
		}
		keyHash = auth.HMACKey(u.Cfg.AppSecret, req.PrivateKey)
	}
	enc := 0
	if req.EncryptedFlag == "1" || req.EncryptedFlag == "true" {
		enc = 1
	}
	expHours, _ := strconv.Atoi(req.ExpiryHours)
	if expHours <= 0 {
		expHours = 6
	}
	maxHours := 24
	if req.Admin {
		maxHours = 24 * 90
	}
	if expHours > maxHours {
		expHours = maxHours
	}
	exp := time.Now().Add(time.Duration(expHours) * time.Hour).UTC().Format(time.RFC3339Nano)

	capMu.Lock()
	defer capMu.Unlock()

	// Cap precheck.
	used := storage.UsedBytes(u.Cfg.BlobDir)
	if used >= u.Cfg.StorageCapBytes {
		audit.Log(u.DB, "public", ip, "upload_cap_reject", "", fmt.Sprintf("used=%d cap=%d", used, u.Cfg.StorageCapBytes))
		return Result{}, ErrCap
	}

	// Store blob.
	id := storage.UUID()
	path, sum, size, err := storage.Store(u.Cfg.BlobDir, id, req.Reader, u.Cfg.MaxUploadBytes)
	if err != nil {
		if errors.Is(err, storage.ErrTooLarge) {
			return Result{}, ErrTooLarge
		}
		return Result{}, ErrStore
	}
	if used+size > u.Cfg.StorageCapBytes {
		purgeOne(path)
		audit.Log(u.DB, "public", ip, "upload_cap_reject", id, fmt.Sprintf("%d + %d > %d", used, size, u.Cfg.StorageCapBytes))
		return Result{}, ErrCap
	}

	// Insert metadata; roll back blob on failure.
	sh := share.Share{
		ID: id, Title: title, Visibility: vis, PrivateKeyHash: keyHash,
		Encrypted: enc == 1, CipherMeta: req.CipherMeta, ZipManifest: manifestForInsert(enc, req.ZipManifest),
		Size: size, BlobPath: path, BlobSHA256: sum, UploaderIP: ip,
		ExpiresAt: sql.NullString{String: exp, Valid: true},
	}
	if err := u.Store.Insert(sh); err != nil {
		purgeOne(path)
		return Result{}, ErrStore
	}
	audit.Log(u.DB, "public", ip, "upload", id, fmt.Sprintf("size=%d visibility=%s encrypted=%d", size, vis, enc))
	return Result{ID: id, URL: "/s/" + id}, nil
}

// manifestForInsert drops the plaintext ZIP manifest for encrypted shares so
// file names/sizes/types are not readable without the password. The browser
// already sends "[]" for encrypted uploads, but the server enforces it so a
// malicious client cannot leak the manifest by sending it anyway.
func manifestForInsert(encrypted int, raw string) string {
	if encrypted == 1 {
		return "[]"
	}
	return raw
}

// purgeOne removes a blob path, best-effort, mirroring the prior handler helper.
func purgeOne(path string) { _ = os.Remove(filepath.Clean(path)) }
