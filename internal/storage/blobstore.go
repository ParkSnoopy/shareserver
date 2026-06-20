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

func Store(dir, id string, r io.Reader, limit int64) (path, sum string, size int64, err error) {
	if err = os.MkdirAll(dir, 0755); err != nil {
		return
	}
	tmp := filepath.Join(dir, id+".tmp")
	final := filepath.Join(dir, id+".blob")
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	defer os.Remove(tmp)
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
	if err = os.Rename(tmp, final); err != nil {
		return
	}
	return final, hex.EncodeToString(h.Sum(nil)), size, nil
}

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
