package internaltest

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"shareserver/internal/auth"
	httpx "shareserver/internal/http"
	"shareserver/internal/share"
)

func TestAPIUploadStoresEncryptedPayloadWithSeparatePasswordHash(t *testing.T) {
	a, router := newRouter(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	fields := map[string]string{
		"title":        "api payload",
		"visibility":   "public",
		"password":     "download-password",
		"encrypted":    "1",
		"cipher_meta":  testCipherMeta,
		"zip_manifest": "[]",
		"expiry_hours": "6",
	}
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("blob", "payload.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("client-encrypted-payload")); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/upload", &body)
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body=%q", w.Code, w.Body.String())
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("stateless API created browser session: %q", w.Header().Get("Set-Cookie"))
	}
	var response struct {
		ID          string `json:"id"`
		URL         string `json:"url"`
		DownloadURL string `json:"download_url"`
		Encryption  string `json:"encryption"`
		Size        int64  `json:"size"`
		ExpiresAt   string `json:"expires_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID == "" || response.URL != "/s/"+response.ID || response.DownloadURL != "/api/v0/download/"+response.ID || response.Size != int64(len("client-encrypted-payload")) || response.ExpiresAt == "" {
		t.Fatalf("incomplete upload response: %+v", response)
	}
	stored, ok := share.NewStore(a.DB).Get(response.ID)
	if !ok {
		t.Fatal("uploaded Share missing")
	}
	if !stored.Encrypted || stored.DownloadPasswordHash == "" || stored.DownloadPasswordHash == fields["password"] {
		t.Fatalf("download verifier not stored safely: %+v", stored)
	}
	if !auth.CheckDownloadPassword(stored.DownloadPasswordHash, fields["password"]) {
		t.Fatal("stored download verifier does not match password")
	}
	if response.Encryption != "client" {
		t.Fatalf("client upload encryption = %q, want client", response.Encryption)
	}
}

func TestAPIPlainUploadReturnsGeneratedEncryptionMetadata(t *testing.T) {
	a, router := newRouter(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"title": "plain API payload", "visibility": "public", "password": "download-password",
		"encrypted": "0", "expiry_hours": "6",
	} {
		if err := form.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("blob", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("plain API content")); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/upload", &body)
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("plain upload status = %d, body=%q", w.Code, w.Body.String())
	}
	var response struct {
		ID         string           `json:"id"`
		Encryption string           `json:"encryption"`
		CipherMeta serverCipherMeta `json:"cipher_meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID == "" || response.Encryption != "server" || response.CipherMeta.Cipher != "AES-256-GCM-CHUNKED" {
		t.Fatalf("plain upload response incomplete: %+v", response)
	}
	stored, ok := share.NewStore(a.DB).Get(response.ID)
	if !ok || stored.CipherMeta == "" {
		t.Fatal("server-encrypted Share missing")
	}
}

func TestAPIClientUploadAndDownloadAcceptBrowserPasswordHash(t *testing.T) {
	a, router := newRouter(t)
	passwordHash := auth.DownloadPasswordToken("browser-only-password")
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"title": "browser payload", "visibility": "public", "password_hash": passwordHash,
		"encrypted": "1", "cipher_meta": testCipherMeta, "expiry_hours": "6",
	} {
		if err := form.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("blob", "browser.payload")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("browser-encrypted-payload")); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/upload", &body)
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("browser-hash upload status = %d, body=%q", w.Code, w.Body.String())
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	stored, ok := share.NewStore(a.DB).Get(response.ID)
	if !ok || !auth.CheckDownloadPasswordToken(stored.DownloadPasswordHash, passwordHash) {
		t.Fatal("browser authorization hash verifier missing")
	}
	download := httptest.NewRequest(http.MethodPost, "/api/v0/download/"+response.ID, strings.NewReader(`{"password_hash":"`+passwordHash+`"}`))
	download.TLS = &tls.ConnectionState{}
	download.Header.Set("Content-Type", "application/json")
	downloadResponse := httptest.NewRecorder()
	router.ServeHTTP(downloadResponse, download)
	if downloadResponse.Code != http.StatusOK || downloadResponse.Body.String() != "browser-encrypted-payload" {
		t.Fatalf("browser-hash download status = %d, body=%q", downloadResponse.Code, downloadResponse.Body.String())
	}
}

func TestAPIPlainUploadRejectsOversizedSourceWithoutStoredBlob(t *testing.T) {
	a := newTestApp(t)
	a.C.MaxUploadBytes = 1
	router := httpx.New(a)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"title": "large plain payload", "visibility": "public", "password": "download-password",
		"encrypted": "0", "expiry_hours": "6",
	} {
		if err := form.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("blob", "large.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("x"), (4<<20)+1024)); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/upload", &body)
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized plain upload status = %d, body=%q", w.Code, w.Body.String())
	}
	if count := countBlobs(t, a.C.BlobDir); count != 0 {
		t.Fatalf("oversized plain upload stored %d blobs", count)
	}
}

func TestAPIRejectsOversizedMetadataInsteadOfTruncatingIt(t *testing.T) {
	a, router := newRouter(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"title": "large metadata", "visibility": "private", "password": "download-password",
		"private_key": strings.Repeat("k", (64<<10)+1), "encrypted": "0", "expiry_hours": "6",
	} {
		if err := form.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := form.CreateFormFile("blob", "payload.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("plain content")); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/upload", &body)
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized metadata status = %d, body=%q", w.Code, w.Body.String())
	}
	if count := countBlobs(t, a.C.BlobDir); count != 0 {
		t.Fatalf("oversized metadata stored %d blobs", count)
	}
}

func TestAPIDownloadRateLimitsEachIP(t *testing.T) {
	a := newTestApp(t)
	a.C.DownloadAttemptsPerMinute = 1
	router := httpx.New(a)
	request := func(ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v0/download/00000000-0000-0000-0000-000000000099", strings.NewReader("{\"password\":\"wrong\"}"))
		req.TLS = &tls.ConnectionState{}
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip + ":4321"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if first := request("203.0.113.20"); first.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt status = %d, want 401", first.Code)
	}
	if second := request("203.0.113.20"); second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") != "60" {
		t.Fatalf("limited attempt = %d Retry-After=%q", second.Code, second.Header().Get("Retry-After"))
	}
	if otherIP := request("203.0.113.21"); otherIP.Code != http.StatusUnauthorized {
		t.Fatalf("independent IP status = %d, want 401", otherIP.Code)
	}
}

func TestServerRendersAndPersistsSessionLanguageBeforeJavaScript(t *testing.T) {
	_, router := newRouter(t)
	first := httptest.NewRequest(http.MethodGet, "/upload", nil)
	first.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	firstResponse := httptest.NewRecorder()
	router.ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first page status = %d", firstResponse.Code)
	}
	assertBodyContains(t, firstResponse.Body.String(), `<html lang="zh">`, "# 上传", "必填加密密码")
	if strings.Contains(firstResponse.Body.String(), "# Upload") {
		t.Fatal("initial translated HTML still contains English upload heading")
	}
	cookies := firstResponse.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("session cookie missing")
	}

	second := httptest.NewRequest(http.MethodGet, "/upload", nil)
	second.Header.Set("Accept-Language", "en")
	second.AddCookie(cookies[0])
	secondResponse := httptest.NewRecorder()
	router.ServeHTTP(secondResponse, second)
	assertBodyContains(t, secondResponse.Body.String(), `<html lang="zh">`, "# 上传", "必填加密密码")
}

func TestDownloadPasswordHashNormalizesUnicodeIndependently(t *testing.T) {
	hash, err := auth.HashDownloadPassword("e\u0301")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.CheckDownloadPassword(hash, "é") {
		t.Fatal("NFC-equivalent download password did not match")
	}
	adminHash, err := auth.HashPassword("é")
	if err != nil {
		t.Fatal(err)
	}
	if hash == adminHash {
		t.Fatal("download and admin password hashes must use separate processes")
	}
}

func TestAPIDownloadDelayAppliesToDeniedRequests(t *testing.T) {
	_, router := newRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/not-a-uuid", strings.NewReader("{\"password\":\"wrong\"}"))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	started := time.Now()
	router.ServeHTTP(w, req)
	if elapsed := time.Since(started); elapsed < time.Second {
		t.Fatalf("denied download delay = %v, want at least 1s", elapsed)
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("denied download status = %d, want 401", w.Code)
	}
}

func TestAPIRequiresHTTPSWithoutCreatingSession(t *testing.T) {
	a, router := newRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/not-a-uuid", strings.NewReader("{\"password\":\"wrong\"}"))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "sid", Value: "invalid-session"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUpgradeRequired {
		t.Fatalf("plain HTTP API status = %d, want 426", w.Code)
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("rejected API created session cookie: %q", w.Header().Get("Set-Cookie"))
	}
	count, err := a.DB.Session.Query().Count(req.Context())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected API created %d session rows", count)
	}
}

func TestAPITrustsRailwayHTTPSProxy(t *testing.T) {
	t.Setenv("RAILWAY_ENVIRONMENT_ID", "test-environment")
	_, router := newRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/upload", nil)
	req.RemoteAddr = "100.64.0.1:4321"
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("Railway HTTPS proxy status = %d, want 415 after HTTPS check", w.Code)
	}
}
