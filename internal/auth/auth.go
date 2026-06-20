package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"golang.org/x/crypto/bcrypt"
	"math/rand/v2"
	"net"
	"shareserver/internal/db"
	"time"
)

func HashPassword(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(b), err
}
func CheckPassword(hash, p string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(p)) == nil
}

func HMACKey(secret []byte, key string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(key))
	return hex.EncodeToString(m.Sum(nil))
}

func EnsureAdmin(d *sql.DB, user, pass string, syncPassword bool) error {
	var id int64
	err := d.QueryRow(`select id from admins where username=?`, user).Scan(&id)
	if err == nil {
		if syncPassword {
			h, err := HashPassword(pass)
			if err != nil {
				return err
			}
			_, err = d.Exec(`update admins set password_hash=? where id=?`, h, id)
			return err
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	var n int
	_ = d.QueryRow(`select count(*) from admins`).Scan(&n)
	if n > 0 && !syncPassword {
		return nil
	}
	h, err := HashPassword(pass)
	if err != nil {
		return err
	}
	_, err = d.Exec(`insert into admins(username,password_hash,created_at) values(?,?,?)`, user, h, db.Now())
	return err
}

func IsBanned(d *sql.DB, ip string, now time.Time) bool {
	var until string
	if d.QueryRow(`select banned_until from ip_bans where ip=?`, ip).Scan(&until) != nil {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, until)
	return err == nil && t.After(now.UTC())
}

func RecordLoginFailure(d *sql.DB, ip string, now time.Time) (banned bool, until time.Time) {
	_, _ = d.Exec(`insert into login_failures(ip,happened_at) values(?,?)`, ip, now.UTC().Format(time.RFC3339Nano))
	cut := now.Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	var n int
	_ = d.QueryRow(`select count(*) from login_failures where ip=? and happened_at>=?`, ip, cut).Scan(&n)
	if n >= 5 {
		until = now.Add(6*time.Hour + time.Duration(rand.IntN(61))*time.Minute).UTC()
		_, _ = d.Exec(`insert into ip_bans(ip,banned_until) values(?,?) on conflict(ip) do update set banned_until=excluded.banned_until`, ip, until.Format(time.RFC3339Nano))
		return true, until
	}
	return false, time.Time{}
}

func ResetFailures(d *sql.DB, ip string) { _, _ = d.Exec(`delete from login_failures where ip=?`, ip) }
func CleanIP(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
}
