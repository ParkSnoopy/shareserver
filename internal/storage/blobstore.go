package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// UUID returns a random RFC 4122 version 4 identifier for share/blob names.
func UUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

var ErrTooLarge = fmt.Errorf("upload too large")

// Stage streams a blob into private staging, hashes it, and enforces size.
// Callers atomically Commit only after their final capacity check.
func Stage(dir, id string, r io.Reader, limit int64) (path, sum string, size int64, err error) {
	staging := filepath.Join(dir, ".staging")
	if err = os.MkdirAll(staging, 0700); err != nil {
		return
	}
	path = filepath.Join(staging, id+".tmp")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(path)
		}
	}()
	defer f.Close()
	h := sha256.New()
	lr := &io.LimitedReader{R: r, N: limit + 1}
	size, err = io.Copy(io.MultiWriter(f, h), lr)
	if err != nil {
		return
	}
	if size > limit {
		err = ErrTooLarge
		return
	}
	if err = f.Close(); err != nil {
		return
	}
	committed = true
	return path, hex.EncodeToString(h.Sum(nil)), size, nil
}

// Commit atomically moves a staged blob into durable storage.
func Commit(staged, dir, id string) (string, error) {
	final := filepath.Join(dir, id+".blob")
	if err := os.Rename(staged, final); err != nil {
		return "", err
	}
	return final, nil
}

// UsedBytes totals committed blob files for storage-cap and admin display.
func UsedBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(p) == ".blob" {
			if st, e := d.Info(); e == nil {
				total += st.Size()
			}
		}
		return nil
	})
	return total
}

// RemoveBlobBestEffort removes a partially stored blob during upload rollback.
func RemoveBlobBestEffort(path string) { _ = os.Remove(filepath.Clean(path)) }
