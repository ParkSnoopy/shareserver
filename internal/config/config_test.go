package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoadProductionSecret(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("DEBUG", "false")
	t.Setenv("APP_SECRET", "")
	t.Setenv("ADMIN_PASSWORD", "test-only-admin-not-a-deployment-credential")
	t.Setenv("DB_PATH", filepath.Join(dir, "data", "test.db"))
	first := Load()
	second := Load()
	if first.Dev || len(first.AppSecret) != 64 || !bytes.Equal(first.AppSecret, second.AppSecret) {
		t.Fatal("production startup must generate and reuse one secret without enabling debug")
	}
	info, err := os.Stat(first.DBPath + ".secret")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("secret permissions = %o, want 600", info.Mode().Perm())
	}
	t.Setenv("APP_SECRET", "explicit-test-only-secret")
	if string(Load().AppSecret) != "explicit-test-only-secret" {
		t.Fatal("explicit environment secret must take precedence")
	}
	stored, err := os.ReadFile(first.DBPath + ".secret")
	if err != nil || !bytes.Equal(stored, first.AppSecret) {
		t.Fatal("explicit override must not replace the generated secret")
	}
}

func TestPersistentSecretConcurrentStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "test.db.secret")
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			secret, err := persistentAppSecret(path)
			if err != nil {
				t.Error(err)
				return
			}
			stored, err := os.ReadFile(path)
			if err != nil || secret != string(stored) {
				t.Error("concurrent startup returned a different key from the published file")
			}
		})
	}
	group.Wait()
}

func TestPersistentSecretRejectsUnsafeState(t *testing.T) {
	for _, kind := range []string{"empty", "corrupt", "oversized", "exposed", "directory", "symlink", "blocked-parent"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "test.db.secret")
			original := []byte("test-only-corrupt-key")
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "blocked-parent":
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(path, "test.db.secret")
			default:
				if kind == "empty" {
					original = nil
				}
				if kind == "corrupt" {
					original = []byte(strings.Repeat("g", 64))
				}
				if kind == "oversized" {
					original = []byte(strings.Repeat("f", 65))
				}
				if kind == "exposed" || kind == "symlink" {
					original = []byte(strings.Repeat("0", 64)) // Test-only key, never a runtime default.
				}
				if err := os.WriteFile(path, original, 0600); err != nil {
					t.Fatal(err)
				}
				if kind == "exposed" {
					if err := os.Chmod(path, 0644); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "symlink" {
					link := filepath.Join(dir, "link.secret")
					if err := os.Symlink(path, link); err != nil {
						t.Fatal(err)
					}
					path = link
				}
			}
			if _, err := persistentAppSecret(path); err == nil {
				t.Fatal("unsafe persisted state must fail instead of generating a replacement")
			}
			if kind != "directory" && kind != "blocked-parent" {
				stored, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(stored, original) {
					t.Fatal("failed startup modified the persisted key")
				}
			}
		})
	}
}
