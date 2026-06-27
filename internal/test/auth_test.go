package internaltest

import (
	"context"
	"testing"
	"time"

	"shareserver/internal/auth"
	"shareserver/internal/ent"
	"shareserver/internal/ent/loginfailureevent"
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

func TestAdminLoginBadCredentialsBanIP(t *testing.T) {
	ctx := context.Background()
	client := newClient(t)
	insertLoginAdmin(t, client, "admin", "correct")
	ip := "203.0.113.10"
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	var result auth.AdminLoginResult
	for i := 0; i < 5; i++ {
		result = auth.AdminLogin(ctx, client, ip, "admin", "wrong", now.Add(time.Duration(i)*time.Second))
		if result.Status != auth.AdminLoginFailed {
			t.Fatalf("attempt %d status = %v, want failed", i+1, result.Status)
		}
	}
	if result.BannedUntil.IsZero() {
		t.Fatalf("fifth failure did not report ban")
	}
	ban, err := client.IpBan.Get(ctx, ip)
	if err != nil {
		t.Fatalf("ban row: %v", err)
	}
	if ban.BannedUntil != result.BannedUntil.Format(time.RFC3339Nano) {
		t.Fatalf("ban until = %q, want %q", ban.BannedUntil, result.BannedUntil.Format(time.RFC3339Nano))
	}
}

func TestAdminLoginBannedIPStopsBeforeFailureRecording(t *testing.T) {
	ctx := context.Background()
	client := newClient(t)
	ip := "203.0.113.11"
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	_, err := client.IpBan.Create().
		SetID(ip).
		SetBannedUntil(now.Add(time.Hour).Format(time.RFC3339Nano)).
		Save(ctx)
	if err != nil {
		t.Fatal(err)
	}

	result := auth.AdminLogin(ctx, client, ip, "missing", "bad", now)
	if result.Status != auth.AdminLoginBanned {
		t.Fatalf("status = %v, want banned", result.Status)
	}
	if got := countLoginFailures(t, client, ip); got != 0 {
		t.Fatalf("failure records = %d, want 0", got)
	}
}

func TestAdminLoginSuccessResetsFailuresAndReturnsAdminID(t *testing.T) {
	ctx := context.Background()
	client := newClient(t)
	adminID := insertLoginAdmin(t, client, "admin", "correct")
	ip := "203.0.113.12"
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		_, err := client.LoginFailureEvent.Create().
			SetIP(ip).
			SetHappenedAt(now.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)).
			Save(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}

	result := auth.AdminLogin(ctx, client, ip, "admin", "correct", now.Add(3*time.Second))
	if result.Status != auth.AdminLoginSuccess {
		t.Fatalf("status = %v, want success", result.Status)
	}
	if result.AdminID != adminID {
		t.Fatalf("admin id = %d, want %d", result.AdminID, adminID)
	}
	if got := countLoginFailures(t, client, ip); got != 0 {
		t.Fatalf("failure records = %d, want 0", got)
	}
}

func insertLoginAdmin(t *testing.T, client *ent.Client, username, password string) int {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	adminRow, err := client.Admin.Create().
		SetUsername(username).
		SetPasswordHash(hash).
		SetCreatedAt(time.Now().UTC().Format(time.RFC3339Nano)).
		Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return adminRow.ID
}

func countLoginFailures(t *testing.T, client *ent.Client, ip string) int {
	t.Helper()
	count, err := client.LoginFailureEvent.Query().
		Where(loginfailureevent.IP(ip)).
		Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return count
}
