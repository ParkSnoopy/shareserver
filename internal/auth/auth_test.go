package auth

import "testing"

func TestHMACKeyStableAndSecretScoped(t *testing.T) {
	a := HMACKey([]byte("secret-a"), "key")
	b := HMACKey([]byte("secret-a"), "key")
	c := HMACKey([]byte("secret-b"), "key")
	if a == "" || a != b {
		t.Fatalf("hmac not stable")
	}
	if a == c {
		t.Fatalf("hmac must depend on app secret")
	}
}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "pw") {
		t.Fatalf("password should match")
	}
	if CheckPassword(h, "bad") {
		t.Fatalf("bad password matched")
	}
}
