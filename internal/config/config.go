package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
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
	loadDotEnv(".env")
	dev := envBool("DEBUG", true)
	secret := os.Getenv("APP_SECRET")
	if secret == "" {
		if !dev {
			log.Fatal("APP_SECRET required when DEBUG is not 1/true")
		}
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			log.Fatal(err)
		}
		secret = hex.EncodeToString(b)
		log.Println("warning: using ephemeral dev APP_SECRET")
	}
	adminPassword, hasAdminPassword := os.LookupEnv("ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = "admin-change-me"
	}
	if !dev && (!hasAdminPassword || adminPassword == "admin-change-me") {
		log.Fatal("ADMIN_PASSWORD required when DEBUG is not 1/true")
	}
	loc, err := time.LoadLocation(env("TZ", "Asia/Shanghai"))
	if err != nil {
		log.Fatal(err)
	}
	return Config{
		Addr: env("ADDR", ":8080"), DBPath: env("DB_PATH", "data/shareserver.db"), BlobDir: env("BLOB_DIR", "data/blobs"),
		AppSecret: []byte(secret), AdminUser: env("ADMIN_USER", "admin"), AdminPassword: adminPassword,
		MaxUploadBytes: int64(envInt("MAX_UPLOAD_BYTES", 200*1024*1024)), StorageCapBytes: int64(envInt("STORAGE_CAP_BYTES", 400*1024*1024)),
		TrustProxyHeaders: envBool("TRUST_PROXY_HEADERS", false), TZ: loc, Dev: dev,
	}
}

// loadDotEnv imports simple KEY=value pairs without overriding real environment values.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
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
