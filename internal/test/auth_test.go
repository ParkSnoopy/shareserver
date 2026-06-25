package internaltest

import (
	"testing"

	"shareserver/internal/auth"
)

func TestHMACKeyStableAndSecretScoped(t *testing.T) {
	a := auth.HMACKey([]byte("secret-a"), "key")
	b := auth.HMACKey([]byte("secret-a"), "key")
	c := auth.HMACKey([]byte("secret-b"), "key")
	if a == "" || a != b {
		t.Fatalf("hmac not stable")
	}
	if a == c {
		t.Fatalf("hmac must depend on app secret")
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.CheckPassword(h, "pw") {
		t.Fatalf("password should match")
	}
	if auth.CheckPassword(h, "bad") {
		t.Fatalf("bad password matched")
	}
}
