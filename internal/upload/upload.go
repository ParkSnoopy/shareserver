// Package upload owns the upload policy: validation, cap enforcement, blob
// storage, rollback, metadata insert, and audit. The HTTP handler parses
// multipart only; every rule that can fail lives here.
package upload

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	maxPasswordBytes = 4 << 10
)

var (
	capMu        sync.Mutex
	hashMu       sync.Mutex
	reservations = map[string]int64{}
)

var (
	ErrTooLarge            = errors.New("upload too large")
	ErrCap                 = errors.New("storage cap reached")
	ErrStore               = errors.New("store failed")
	ErrPrivateKeyRequired  = errors.New("private key required")
	ErrPasswordRequired    = errors.New("download password required")
	ErrPasswordHashInvalid = errors.New("invalid download password hash")
	ErrEncryptionRequired  = errors.New("invalid encryption mode")
	ErrMetadataTooLarge    = errors.New("metadata too large")
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
	Cfg       Config
	Store     *share.Store
	Integrity *storage.Integrity
	DB        *ent.Client // for audit only
}

// Request is the parsed multipart form plus the blob reader.
type Request struct {
	Title, Visibility, PrivateKey, DownloadPassword, DownloadPasswordToken string
	CipherMeta, ZipManifest, EncryptedFlag, ExpiryHours                    string
	Filename                                                               string
	Reader                                                                 io.Reader
	UploaderIP                                                             string
	Admin                                                                  bool
}

// Result is what a successful upload yields to the handler.
type Result struct {
	ID, URL, DownloadURL, ExpiresAt, CipherMeta, Encryption string
	Size                                                    int64
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
	if len(title) > maxTitleBytes || len(req.DownloadPassword) > maxPasswordBytes || len(req.DownloadPasswordToken) > maxPasswordBytes || len(req.CipherMeta) > maxCipherBytes || len(req.ZipManifest) > maxManifestBytes {
		return Result{}, ErrMetadataTooLarge
	}
	clientEncrypted := req.EncryptedFlag == "1" || req.EncryptedFlag == "true"
	serverEncrypted := req.EncryptedFlag == "0" || req.EncryptedFlag == "false"
	if !clientEncrypted && !serverEncrypted {
		return Result{}, ErrEncryptionRequired
	}
	if clientEncrypted && !validCipherMeta(req.CipherMeta) {
		return Result{}, ErrEncryptionRequired
	}
	if serverEncrypted && req.CipherMeta != "" {
		return Result{}, ErrEncryptionRequired
	}
	if clientEncrypted && (req.DownloadPassword == "") == (req.DownloadPasswordToken == "") {
		return Result{}, ErrPasswordRequired
	}
	if req.DownloadPasswordToken != "" && !auth.ValidDownloadPasswordToken(req.DownloadPasswordToken) {
		return Result{}, ErrPasswordHashInvalid
	}
	if serverEncrypted && (req.DownloadPassword == "" || req.DownloadPasswordToken != "") {
		return Result{}, ErrPasswordRequired
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
	enc := 1
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

	// Unprotected legacy and expired Shares must release their blobs and metadata
	// before capacity is measured, otherwise unreachable storage can reject an upload that fits.
	u.Integrity.PurgeUnprotected()
	u.Integrity.Purge(time.Now().UTC())

	reservation, ok := reserveCapacity(u.Cfg.BlobDir, u.Cfg.MaxUploadBytes, u.Cfg.StorageCapBytes)
	if !ok {
		used := storage.UsedBytes(u.Cfg.BlobDir)
		audit.Log(u.DB, "public", ip, "upload_cap_reject", "", fmt.Sprintf("used=%d cap=%d", used, u.Cfg.StorageCapBytes))
		return Result{}, ErrCap
	}
	defer releaseCapacity(u.Cfg.BlobDir, reservation)

	// Serialize password KDFs behind capacity reservation so anonymous uploads
	// cannot run unbounded bcrypt/PBKDF2 work in parallel or while storage is full.
	hashMu.Lock()
	var downloadPasswordHash string
	var err error
	if req.DownloadPasswordToken != "" {
		downloadPasswordHash, err = auth.HashDownloadPasswordToken(req.DownloadPasswordToken)
	} else {
		downloadPasswordHash, err = auth.HashDownloadPassword(req.DownloadPassword)
	}
	if err != nil {
		hashMu.Unlock()
		return Result{}, ErrStore
	}
	reader := req.Reader
	cipherMeta := req.CipherMeta
	encryption := "client"
	if serverEncrypted {
		encryptedReader, generatedMeta, err := encryptPlainUpload(req.Reader, req.Filename, req.DownloadPassword)
		if err != nil {
			hashMu.Unlock()
			return Result{}, ErrStore
		}
		defer encryptedReader.Close()
		reader = encryptedReader
		cipherMeta = generatedMeta
		encryption = "server"
	}
	hashMu.Unlock()

	// Stage encrypted bytes without holding capacity/storage locks. Slow clients
	// cannot block unrelated uploads or reconciliation while sending a blob.
	id := storage.UUID()
	stagedPath, sum, size, err := storage.Stage(u.Cfg.BlobDir, id, reader, reservation)
	if err != nil {
		if errors.Is(err, storage.ErrTooLarge) {
			if reservation < u.Cfg.MaxUploadBytes {
				return Result{}, ErrCap
			}
			return Result{}, ErrTooLarge
		}
		return Result{}, ErrStore
	}
	defer storage.RemoveBlobBestEffort(stagedPath)
	if size < 16 {
		return Result{}, ErrEncryptionRequired
	}

	capMu.Lock()
	defer capMu.Unlock()
	u.Integrity.PurgeUnprotected()
	u.Integrity.Purge(time.Now().UTC())
	unlockStorage := u.Integrity.Lock()
	defer unlockStorage()
	used := storage.UsedBytes(u.Cfg.BlobDir)
	otherReservations := reservations[u.Cfg.BlobDir] - reservation
	if used+size+otherReservations > u.Cfg.StorageCapBytes {
		audit.Log(u.DB, "public", ip, "upload_cap_reject", id, fmt.Sprintf("%d + %d > %d", used, size, u.Cfg.StorageCapBytes))
		return Result{}, ErrCap
	}
	path, err := storage.Commit(stagedPath, u.Cfg.BlobDir, id)
	if err != nil {
		return Result{}, ErrStore
	}

	// Insert metadata; roll back blob on failure.
	sh := share.Share{
		ID: id, Title: title, Visibility: vis, PrivateKeyHash: keyHash, DownloadPasswordHash: downloadPasswordHash,
		Encrypted: enc == 1, CipherMeta: cipherMeta, ZipManifest: manifestForInsert(enc, req.ZipManifest),
		Size: size, BlobPath: path, BlobSHA256: sum, UploaderIP: ip,
		ExpiresAt: sql.NullString{String: exp, Valid: true},
	}
	if err := u.Store.Insert(sh); err != nil {
		storage.RemoveBlobBestEffort(path)
		return Result{}, ErrStore
	}
	audit.Log(u.DB, "public", ip, "upload", id, fmt.Sprintf("size=%d visibility=%s encrypted=%d encryption=%s", size, vis, enc, encryption))
	return Result{
		ID: id, URL: "/s/" + id, DownloadURL: "/api/v0/download/" + id,
		Size: size, ExpiresAt: exp, CipherMeta: cipherMeta, Encryption: encryption,
	}, nil
}

func reserveCapacity(dir string, maximum, capacity int64) (int64, bool) {
	capMu.Lock()
	defer capMu.Unlock()
	available := capacity - storage.UsedBytes(dir) - reservations[dir]
	if available <= 0 || maximum <= 0 {
		return 0, false
	}
	if maximum < available {
		available = maximum
	}
	reservations[dir] += available
	return available, true
}

func releaseCapacity(dir string, reserved int64) {
	capMu.Lock()
	defer capMu.Unlock()
	reservations[dir] -= reserved
	if reservations[dir] <= 0 {
		delete(reservations, dir)
	}
}

type cipherMetadata struct {
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Cipher     string `json:"cipher"`
	Nonce      string `json:"nonce"`
}

// validCipherMeta rejects uploads that do not describe the browser's required
// encrypted wire format. Payload bytes remain opaque and are never decrypted.
func validCipherMeta(raw string) bool {
	var meta cipherMetadata
	if json.Unmarshal([]byte(raw), &meta) != nil || meta.KDF != "PBKDF2-SHA-384" || meta.Cipher != "AES-256-GCM" || meta.Iterations < 100000 || meta.Iterations > 1200000 {
		return false
	}
	salt, saltErr := base64.StdEncoding.DecodeString(meta.Salt)
	nonce, nonceErr := base64.StdEncoding.DecodeString(meta.Nonce)
	return saltErr == nil && nonceErr == nil && len(salt) == 16 && len(nonce) == 12
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
