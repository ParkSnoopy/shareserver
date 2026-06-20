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

func Load() Config {
	loadDotEnv(".env")
	dev := env("ENV", "dev") == "dev"
	secret := os.Getenv("APP_SECRET")
	if secret == "" {
		if !dev {
			log.Fatal("APP_SECRET required when ENV != dev")
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
		log.Fatal("ADMIN_PASSWORD required when ENV != dev")
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

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func envBool(k string, d bool) bool {
	if v := os.Getenv(k); v != "" {
		return v == "1" || v == "true" || v == "yes"
	}
	return d
}
func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return d
}
