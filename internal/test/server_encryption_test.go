package internaltest

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"

	"testing"

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/text/unicode/norm"
	"shareserver/internal/upload"
)

type serverCipherMeta struct {
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Cipher     string `json:"cipher"`
	Nonce      string `json:"nonce"`
	ChunkSize  int    `json:"chunk_size"`
}

func TestPlainUploadIsZippedAndEncryptedOnServer(t *testing.T) {
	uploader, store, _ := newUploader(t, 1<<30)
	want := make([]byte, (2<<20)+123)
	if _, err := rand.Read(want); err != nil {
		t.Fatal(err)
	}
	result, err := uploader.Do(upload.Request{
		Title: "plain", Visibility: "public", ExpiryHours: "6",
		DownloadPassword: "cafe\u0301", EncryptedFlag: "0", Filename: "../../notes.txt",
		Reader: bytes.NewReader(want), UploaderIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("plain upload failed: %v", err)
	}
	if result.Encryption != "server" || result.CipherMeta == "" {
		t.Fatalf("server encryption result incomplete: %+v", result)
	}
	stored, ok := store.Get(result.ID)
	if !ok || !stored.Encrypted || stored.CipherMeta != result.CipherMeta {
		t.Fatalf("server-encrypted Share metadata mismatch: %+v", stored)
	}
	ciphertext, err := os.ReadFile(stored.BlobPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, want[:32]) {
		t.Fatal("stored payload contains plaintext")
	}

	archiveBytes, err := decryptServerCiphertext(ciphertext, "café", result.CipherMeta)
	if err != nil {
		t.Fatalf("decrypt server payload: %v", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatalf("open server ZIP: %v", err)
	}
	if len(archive.File) != 1 || archive.File[0].Name != "notes.txt" {
		t.Fatalf("server ZIP entries = %+v", archive.File)
	}
	entry, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	plain, err := io.ReadAll(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, want) {
		t.Fatalf("decrypted payload size = %d, want %d", len(plain), len(want))
	}
}

func TestServerEncryptedPayloadRejectsTampering(t *testing.T) {
	uploader, store, _ := newUploader(t, 1<<30)
	result, err := uploader.Do(upload.Request{
		Title: "plain", Visibility: "public", ExpiryHours: "6",
		DownloadPassword: "password", EncryptedFlag: "false", Filename: "payload.bin",
		Reader: strReader("plain payload for tamper test"), UploaderIP: "1.2.3.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := store.Get(result.ID)
	ciphertext, err := os.ReadFile(stored.BlobPath)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	if _, err := decryptServerCiphertext(ciphertext, "password", result.CipherMeta); err == nil {
		t.Fatal("tampered server ciphertext decrypted")
	}
}

func decryptServerCiphertext(body []byte, password, rawMetadata string) ([]byte, error) {
	var metadata serverCipherMeta
	if err := json.Unmarshal([]byte(rawMetadata), &metadata); err != nil {
		return nil, err
	}
	if metadata.KDF != "PBKDF2-SHA-384" || metadata.Cipher != "AES-256-GCM-CHUNKED" || metadata.ChunkSize != 1<<20 {
		return nil, errors.New("unexpected server cipher metadata")
	}
	salt, err := base64.StdEncoding.DecodeString(metadata.Salt)
	if err != nil {
		return nil, err
	}
	prefix, err := base64.StdEncoding.DecodeString(metadata.Nonce)
	if err != nil || len(prefix) != 8 {
		return nil, errors.New("invalid nonce prefix")
	}
	key := pbkdf2.Key([]byte(norm.NFC.String(password)), salt, metadata.Iterations, 32, sha512.New384)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	offset := 0
	var index uint32
	finalSeen := false
	for offset < len(body) {
		if len(body)-offset < 5 || finalSeen {
			return nil, errors.New("invalid frame header")
		}
		header := body[offset : offset+5]
		offset += 5
		size := int(binary.BigEndian.Uint32(header[1:]))
		final := header[0] == 1
		if (header[0] != 0 && !final) || size <= 0 || size > metadata.ChunkSize || (!final && size != metadata.ChunkSize) {
			return nil, errors.New("invalid frame")
		}
		encryptedSize := size + aead.Overhead()
		if len(body)-offset < encryptedSize {
			return nil, errors.New("truncated frame")
		}
		nonce := make([]byte, aead.NonceSize())
		copy(nonce, prefix)
		binary.BigEndian.PutUint32(nonce[8:], index)
		plain, err := aead.Open(nil, nonce, body[offset:offset+encryptedSize], header)
		if err != nil {
			return nil, err
		}
		offset += encryptedSize
		output.Write(plain)
		finalSeen = final
		index++
	}
	if !finalSeen {
		return nil, errors.New("missing final frame")
	}
	return output.Bytes(), nil
}
