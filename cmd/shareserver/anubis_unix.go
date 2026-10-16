//go:build unix

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"syscall"

	"shareserver/internal/config"
)

// Replace this process so the supervisor still owns Anubis directly. Derive an
// independent signing seed without printing secrets or adding a second key file.
func execAnubis(c config.Config) {
	if len(os.Args) != 2 {
		log.Fatal("unexpected Anubis arguments")
	}
	if os.Getenv("HS512_SECRET") == "" && os.Getenv("ED25519_PRIVATE_KEY_HEX") == "" && os.Getenv("ED25519_PRIVATE_KEY_HEX_FILE") == "" {
		key := hmac.New(sha256.New, c.AppSecret)
		_, _ = key.Write([]byte("shareserver/anubis/ed25519/v1"))
		if err := os.Setenv("ED25519_PRIVATE_KEY_HEX", hex.EncodeToString(key.Sum(nil))); err != nil {
			log.Fatal(err)
		}
	}
	// #nosec G204 -- Fixed executable and argv; only the environment contains runtime configuration.
	log.Fatal(syscall.Exec("./anubis", []string{"./anubis"}, os.Environ()))
}
