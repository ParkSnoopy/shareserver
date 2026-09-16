package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/rand/v2"
	"net"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/text/unicode/norm"
	"shareserver/internal/db"
	"shareserver/internal/ent"
	entadmin "shareserver/internal/ent/admin"
	"shareserver/internal/ent/loginfailureevent"
)

// HashPasswordHash stores a slow, salted verifier for one canonical Base64
// SHA-256 value. HTTP requests never pass plaintext passwords to this package.
func HashPasswordHash(passwordHash string) (string, error) {
	digest, ok := decodePasswordHash(passwordHash)
	if !ok {
		return "", errors.New("invalid password hash")
	}
	b, err := bcrypt.GenerateFromPassword(digest, bcrypt.DefaultCost)
	return string(b), err
}

// CheckPasswordHash compares a canonical Base64 SHA-256 value with its stored verifier.
func CheckPasswordHash(verifier, passwordHash string) bool {
	digest, ok := decodePasswordHash(passwordHash)
	return ok && bcrypt.CompareHashAndPassword([]byte(verifier), digest) == nil
}

// ValidPasswordHash checks canonical Base64 SHA-256 encoding.
func ValidPasswordHash(passwordHash string) bool {
	_, ok := decodePasswordHash(passwordHash)
	return ok
}

func decodePasswordHash(passwordHash string) ([]byte, bool) {
	digest, err := base64.StdEncoding.DecodeString(passwordHash)
	return digest, err == nil && len(digest) == sha256.Size && base64.StdEncoding.EncodeToString(digest) == passwordHash
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
func AdminLogin(ctx context.Context, client *ent.Client, ip, username, passwordHash string, now time.Time) AdminLoginResult {
	now = now.UTC()
	if isBanned(ctx, client, ip, now) {
		return AdminLoginResult{Status: AdminLoginBanned}
	}
	adminRow, err := client.Admin.Query().
		Where(entadmin.UsernameEQ(username)).
		Only(ctx)
	if err != nil || !CheckPasswordHash(adminRow.PasswordHash, passwordHash) {
		_, until := recordLoginFailure(ctx, client, ip, now)
		return AdminLoginResult{Status: AdminLoginFailed, BannedUntil: until}
	}
	resetFailures(ctx, client, ip)
	return AdminLoginResult{Status: AdminLoginSuccess, AdminID: adminRow.ID}
}

// EnsureAdmin derives the configured plaintext password once at startup. HTTP
// authentication still accepts only the derived SHA-256 value.
func EnsureAdmin(client *ent.Client, user, password string, syncPassword bool) error {
	passwordHash := adminPasswordHash(password)
	ctx := context.Background()
	a, err := client.Admin.Query().Where(entadmin.UsernameEQ(user)).Only(ctx)
	if err == nil {
		if CheckPasswordHash(a.PasswordHash, passwordHash) {
			return nil
		}
		legacyMatches := bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(password)) == nil
		if !syncPassword && !legacyMatches {
			return nil
		}
		h, err := HashPasswordHash(passwordHash)
		if err != nil {
			return err
		}
		return client.Admin.UpdateOneID(a.ID).SetPasswordHash(h).Exec(ctx)
	}
	if !ent.IsNotFound(err) {
		return err
	}
	n, _ := client.Admin.Query().Count(ctx)
	if n > 0 && !syncPassword {
		return nil
	}
	h, err := HashPasswordHash(passwordHash)
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

func adminPasswordHash(password string) string {
	digest := sha256.Sum256([]byte("shareserver-admin-password\x00" + norm.NFC.String(password)))
	return base64.StdEncoding.EncodeToString(digest[:])
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
		host = hostport
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}
