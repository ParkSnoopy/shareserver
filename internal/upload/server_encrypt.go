package upload

import (
	"archive/zip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/text/unicode/norm"
)

const (
	serverCipherName       = "AES-256-GCM-CHUNKED"
	serverCipherIterations = 600000
	serverCipherChunkSize  = 1 << 20
	serverNoncePrefixSize  = 8
	serverFrameHeaderSize  = 5
	serverFinalFrame       = 1
)

type serverCipherMetadata struct {
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Cipher     string `json:"cipher"`
	Nonce      string `json:"nonce"`
	ChunkSize  int    `json:"chunk_size"`
}

// encryptPlainUpload returns a stream containing one ZIP entry encrypted as
// independently authenticated AES-GCM frames. Streaming bounds server memory
// while retaining end-to-end truncation and reordering detection.
func encryptPlainUpload(source io.Reader, filename, password string) (io.ReadCloser, string, error) {
	salt := make([]byte, 16)
	noncePrefix := make([]byte, serverNoncePrefixSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, "", err
	}
	if _, err := rand.Read(noncePrefix); err != nil {
		return nil, "", err
	}
	key := pbkdf2.Key([]byte(norm.NFC.String(password)), salt, serverCipherIterations, 32, sha512.New384)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", err
	}
	metadata, err := json.Marshal(serverCipherMetadata{
		KDF: "PBKDF2-SHA-384", Iterations: serverCipherIterations,
		Salt: base64.StdEncoding.EncodeToString(salt), Cipher: serverCipherName,
		Nonce: base64.StdEncoding.EncodeToString(noncePrefix), ChunkSize: serverCipherChunkSize,
	})
	if err != nil {
		return nil, "", err
	}

	zipReader, zipWriter := io.Pipe()
	go writeSingleFileZIP(zipWriter, source, safeArchiveName(filename))
	encryptedReader, encryptedWriter := io.Pipe()
	go writeEncryptedFrames(encryptedWriter, zipReader, aead, noncePrefix)
	return encryptedReader, string(metadata), nil
}

func writeSingleFileZIP(output *io.PipeWriter, source io.Reader, filename string) {
	archive := zip.NewWriter(output)
	header := &zip.FileHeader{Name: filename, Method: zip.Deflate}
	header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
	entry, err := archive.CreateHeader(header)
	if err == nil {
		_, err = io.Copy(entry, source)
	}
	if closeErr := archive.Close(); err == nil {
		err = closeErr
	}
	_ = output.CloseWithError(err)
}

func writeEncryptedFrames(output *io.PipeWriter, source *io.PipeReader, aead cipher.AEAD, noncePrefix []byte) {
	defer source.Close()
	err := encryptFrames(output, source, aead, noncePrefix)
	_ = output.CloseWithError(err)
}

func encryptFrames(output io.Writer, source io.Reader, aead cipher.AEAD, noncePrefix []byte) error {
	current := make([]byte, serverCipherChunkSize)
	n, readErr := io.ReadFull(source, current)
	if readErr == io.EOF && n == 0 {
		return errors.New("empty encryption stream")
	}
	var index uint32
	for {
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return readErr
		}
		if n < serverCipherChunkSize {
			return writeEncryptedFrame(output, aead, noncePrefix, index, current[:n], true)
		}

		next := make([]byte, serverCipherChunkSize)
		nextSize, nextErr := io.ReadFull(source, next)
		final := nextErr == io.EOF && nextSize == 0
		if err := writeEncryptedFrame(output, aead, noncePrefix, index, current[:n], final); err != nil {
			return err
		}
		if final {
			return nil
		}
		if index == ^uint32(0) {
			return errors.New("encryption frame limit exceeded")
		}
		index++
		current, n, readErr = next, nextSize, nextErr
	}
}

func writeEncryptedFrame(output io.Writer, aead cipher.AEAD, noncePrefix []byte, index uint32, plaintext []byte, final bool) error {
	header := make([]byte, serverFrameHeaderSize)
	if final {
		header[0] = serverFinalFrame
	}
	binary.BigEndian.PutUint32(header[1:], uint32(len(plaintext)))
	nonce := make([]byte, aead.NonceSize())
	copy(nonce, noncePrefix)
	binary.BigEndian.PutUint32(nonce[len(nonce)-4:], index)
	ciphertext := aead.Seal(nil, nonce, plaintext, header)
	if _, err := output.Write(header); err != nil {
		return err
	}
	_, err := output.Write(ciphertext)
	return err
}

func safeArchiveName(filename string) string {
	name := path.Base(strings.ReplaceAll(strings.ToValidUTF8(filename, ""), "\\", "/"))
	name = strings.ReplaceAll(name, "\x00", "")
	if name == "" || name == "." || name == "/" {
		return "upload.bin"
	}
	for len(name) > 255 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}
