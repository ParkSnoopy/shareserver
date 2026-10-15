//go:build unix

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"os/exec"
	"syscall"

	"shareserver/internal/config"
)

// Replace this process so the supervisor still owns Anubis directly. Derive an
// independent signing seed without printing secrets or adding a second key file.
func execAnubis(c config.Config) {
	if os.Getenv("HS512_SECRET") == "" && os.Getenv("ED25519_PRIVATE_KEY_HEX") == "" && os.Getenv("ED25519_PRIVATE_KEY_HEX_FILE") == "" {
		key := hmac.New(sha256.New, c.AppSecret)
		_, _ = key.Write([]byte("shareserver/anubis/ed25519/v1"))
		if err := os.Setenv("ED25519_PRIVATE_KEY_HEX", hex.EncodeToString(key.Sum(nil))); err != nil {
			log.Fatal(err)
		}
	}
	path, err := exec.LookPath("anubis")
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(syscall.Exec(path, append([]string{path}, os.Args[2:]...), os.Environ()))
}
