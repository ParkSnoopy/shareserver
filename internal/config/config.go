package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "time/tzdata"
)

// Config is the validated runtime configuration for the single server process.
type Config struct {
	Addr              string
	DBPath            string
	BlobDir           string
	AppSecret         []byte
	AdminUser         string
	AdminPassword     string
	MaxUploadBytes    int64
	StorageCapBytes   int64
	TrustProxyHeaders bool
	TZ                *time.Location
	Dev               bool
}

// Load reads environment settings, applies safe defaults, and fails closed in prod.
func Load() Config {
	loadDotEnv()
	dev := envBool("DEBUG", true)
	dbPath := env("DB_PATH", "data/shareserver.db")
	adminPassword, hasAdminPassword := os.LookupEnv("ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = "admin-change-me"
	}
	if !dev && (!hasAdminPassword || adminPassword == "admin-change-me") {
		log.Fatal("ADMIN_PASSWORD required when DEBUG is not 1/true")
	}
	secret := os.Getenv("APP_SECRET")
	if secret == "" {
		if !dev {
			var err error
			secret, err = persistentAppSecret(dbPath + ".secret")
			if err != nil {
				log.Fatal("initialize APP_SECRET: ", err)
			}
		} else {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				log.Fatal(err)
			}
			secret = hex.EncodeToString(b)
			log.Println("warning: using ephemeral dev APP_SECRET")
		}
	}
	loc, err := time.LoadLocation(env("TZ", "Asia/Shanghai"))
	if err != nil {
		log.Fatal(err)
	}
	return Config{
		Addr: env("ADDR", "127.0.0.1:8081"), DBPath: dbPath, BlobDir: env("BLOB_DIR", "data/blobs"),
		AppSecret: []byte(secret), AdminUser: env("ADMIN_USER", "admin"), AdminPassword: adminPassword,
		MaxUploadBytes: int64(envInt("MAX_UPLOAD_BYTES", 200*1024*1024)), StorageCapBytes: int64(envInt("STORAGE_CAP_BYTES", 400*1024*1024)),
		TrustProxyHeaders: envBool("TRUST_PROXY_HEADERS", false), TZ: loc, Dev: dev,
	}
}

// persistentAppSecret publishes a complete private key file without replacing one
// created by another startup. Invalid or inaccessible state must never rotate it.
func persistentAppSecret(path string) (secret string, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	name := filepath.Base(path)
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := os.CreateTemp(filepath.Dir(path), ".app-secret-*")
		if createErr != nil {
			return "", createErr
		}
		defer func() {
			if closeErr := file.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
				err = errors.Join(err, closeErr)
			}
			err = errors.Join(err, root.Remove(filepath.Base(file.Name())))
		}()
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return "", err
		}
		if _, err := file.WriteString(hex.EncodeToString(key)); err != nil {
			return "", err
		}
		if err := file.Sync(); err != nil {
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		if err := root.Link(filepath.Base(file.Name()), name); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
		dir, openErr := root.Open(".")
		if openErr != nil {
			return "", openErr
		}
		syncErr := dir.Sync()
		closeErr := dir.Close()
		if err := errors.Join(syncErr, closeErr); err != nil {
			return "", err
		}
		info, err = root.Lstat(name)
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("%s must be a regular owner-only file (mode 0600)", path)
	}
	if info.Size() != 64 {
		return "", fmt.Errorf("%s contains an invalid persisted APP_SECRET; restore it from backup", path)
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return "", err
	}
	key, err := hex.DecodeString(string(data))
	if err != nil || len(key) != 32 {
		return "", fmt.Errorf("%s contains an invalid persisted APP_SECRET; restore it from backup", path)
	}
	return string(data), nil
}

// loadDotEnv imports simple KEY=value pairs without overriding real environment values.
func loadDotEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("error: close dotenv: %v", err)
		}
	}()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		if k != "" {
			if _, exists := os.LookupEnv(k); !exists {
				_ = os.Setenv(k, v)
			}
		}
	}
}

// env returns an environment value or a default when unset.
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// envBool parses common true values and otherwise returns the default.
func envBool(k string, d bool) bool {
	if v := os.Getenv(k); v != "" {
		return v == "1" || v == "true" || v == "yes"
	}
	return d
}

// envInt parses an integer setting and falls back on invalid input.
func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return d
}
