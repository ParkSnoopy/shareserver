package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"math/rand/v2"
	"net"
	"time"

	"golang.org/x/crypto/bcrypt"
	"shareserver/internal/db"
	"shareserver/internal/ent"
	entadmin "shareserver/internal/ent/admin"
	"shareserver/internal/ent/loginfailureevent"
)

// HashPassword creates a bcrypt hash for admin credentials.
func HashPassword(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(b), err
}

// CheckPassword compares a plaintext password with a stored bcrypt hash.
func CheckPassword(hash, p string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(p)) == nil
}

// HMACKey turns a private share key into a stable, secret-scoped lookup hash.
func HMACKey(secret []byte, key string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(key))
	return hex.EncodeToString(m.Sum(nil))
}

// AdminLoginStatus is the decision produced by the admin login flow.
type AdminLoginStatus int

const (
	AdminLoginSuccess AdminLoginStatus = iota
	AdminLoginFailed
	AdminLoginBanned
)

// AdminLoginResult describes the fail-closed result of an admin login attempt.
type AdminLoginResult struct {
	Status      AdminLoginStatus
	AdminID     int
	BannedUntil time.Time
}

// AdminLogin verifies an admin login attempt in fail-closed order.
func AdminLogin(ctx context.Context, client *ent.Client, ip, username, password string, now time.Time) AdminLoginResult {
	now = now.UTC()
	if isBanned(ctx, client, ip, now) {
		return AdminLoginResult{Status: AdminLoginBanned}
	}
	adminRow, err := client.Admin.Query().
		Where(entadmin.UsernameEQ(username)).
		Only(ctx)
	if err != nil || !CheckPassword(adminRow.PasswordHash, password) {
		_, until := recordLoginFailure(ctx, client, ip, now)
		return AdminLoginResult{Status: AdminLoginFailed, BannedUntil: until}
	}
	resetFailures(ctx, client, ip)
	return AdminLoginResult{Status: AdminLoginSuccess, AdminID: adminRow.ID}
}

// EnsureAdmin creates or syncs the bootstrap admin without overwriting prod credentials.
func EnsureAdmin(client *ent.Client, user, pass string, syncPassword bool) error {
	ctx := context.Background()
	a, err := client.Admin.Query().Where(entadmin.UsernameEQ(user)).Only(ctx)
	if err == nil {
		if syncPassword {
			h, err := HashPassword(pass)
			if err != nil {
				return err
			}
			return client.Admin.UpdateOneID(a.ID).SetPasswordHash(h).Exec(ctx)
		}
		return nil
	}
	if !ent.IsNotFound(err) {
		return err
	}
	n, _ := client.Admin.Query().Count(ctx)
	if n > 0 && !syncPassword {
		return nil
	}
	h, err := HashPassword(pass)
	if err != nil {
		return err
	}
	_, err = client.Admin.Create().
		SetUsername(user).
		SetPasswordHash(h).
		SetCreatedAt(db.Now()).
		Save(ctx)
	return err
}

// isBanned reports whether an IP has an active login ban.
func isBanned(ctx context.Context, client *ent.Client, ip string, now time.Time) bool {
	ban, err := client.IpBan.Get(ctx, ip)
	if ent.IsNotFound(err) {
		return false
	}
	if err != nil {
		return true
	}
	t, err := time.Parse(time.RFC3339Nano, ban.BannedUntil)
	return err != nil || t.After(now.UTC())
}

// recordLoginFailure stores a failed login and returns a ban after repeated attempts.
func recordLoginFailure(ctx context.Context, client *ent.Client, ip string, now time.Time) (banned bool, until time.Time) {
	_, _ = client.LoginFailureEvent.Create().
		SetIP(ip).
		SetHappenedAt(now.UTC().Format(time.RFC3339Nano)).
		Save(ctx)
	cut := now.Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	n, _ := client.LoginFailureEvent.Query().
		Where(loginfailureevent.IP(ip), loginfailureevent.HappenedAtGTE(cut)).
		Count(ctx)
	if n >= 5 {
		until = now.Add(6*time.Hour + time.Duration(rand.IntN(61))*time.Minute).UTC()
		bannedUntil := until.Format(time.RFC3339Nano)
		if _, err := client.IpBan.Get(ctx, ip); err == nil {
			_ = client.IpBan.UpdateOneID(ip).SetBannedUntil(bannedUntil).Exec(ctx)
		} else {
			_, _ = client.IpBan.Create().SetID(ip).SetBannedUntil(bannedUntil).Save(ctx)
		}
		return true, until
	}
	return false, time.Time{}
}

// resetFailures clears failed-login history after successful admin auth.
func resetFailures(ctx context.Context, client *ent.Client, ip string) {
	_, _ = client.LoginFailureEvent.Delete().Where(loginfailureevent.IP(ip)).Exec(ctx)
}

// CleanIP strips a port from RemoteAddr-style values when present.
func CleanIP(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
}
