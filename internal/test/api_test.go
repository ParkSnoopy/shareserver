package internaltest

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"shareserver/internal/auth"
	"shareserver/internal/ent/loginfailureevent"
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

func TestAPIDownloadRejectsPlaintextPassword(t *testing.T) {
	a, router := newRouter(t)
	id := "00000000-0000-0000-0000-000000000120"
	insertProtectedShare(t, a, id, "encrypted-payload", futureTS(time.Hour), "correct")
	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/"+id, strings.NewReader(`{"password":"correct"}`))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("plaintext password status = %d, want 401", w.Code)
	}
}

func TestAPIDownloadRejectsAmbiguousFormPasswordHash(t *testing.T) {
	a, router := newRouter(t)
	id := "00000000-0000-0000-0000-000000000123"
	insertProtectedShare(t, a, id, "encrypted-payload", futureTS(time.Hour), "correct")
	passwordHash := url.QueryEscape(auth.DownloadPasswordToken("correct"))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/"+id, strings.NewReader("password_hash="+passwordHash+"&password_hash="+passwordHash))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("duplicate password_hash status = %d, want 401", w.Code)
	}
}

func TestAPIDownloadRejectsQueryPasswordHash(t *testing.T) {
	a, router := newRouter(t)
	id := "00000000-0000-0000-0000-000000000124"
	insertProtectedShare(t, a, id, "encrypted-payload", futureTS(time.Hour), "correct")
	passwordHash := url.QueryEscape(auth.DownloadPasswordToken("correct"))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/"+id+"?password_hash="+passwordHash, nil)
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("query password_hash status = %d, want 401", w.Code)
	}
}

func TestAPIDownloadRejectsUnsupportedContentType(t *testing.T) {
	a, router := newRouter(t)
	id := "00000000-0000-0000-0000-000000000125"
	insertProtectedShare(t, a, id, "encrypted-payload", futureTS(time.Hour), "correct")
	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/"+id, strings.NewReader("password_hash="+url.QueryEscape(auth.DownloadPasswordToken("correct"))))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unsupported content type status = %d, want 401", w.Code)
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

func TestAPIDownloadBansAfterMoreThanTenFailuresAndPersists(t *testing.T) {
	a := newTestApp(t)
	router := httpx.New(a)
	staleFailureIP := "download:203.0.113.250"
	if _, err := a.DB.LoginFailureEvent.Create().
		SetIP(staleFailureIP).
		SetHappenedAt(time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano)).
		Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-0000-0000-000000000122"
	insertProtectedShare(t, a, id, "encrypted-payload", futureTS(time.Hour), "correct")
	request := func(handler http.Handler, ip, password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v0/download/"+id, strings.NewReader(downloadPasswordJSON(password)))
		req.TLS = &tls.ConnectionState{}
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip + ":4321"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	ip := "203.0.113.20"
	for failure := 1; failure <= 10; failure++ {
		if response := request(router, ip, "wrong"); response.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want 401", failure, response.Code)
		}
	}
	staleFailures, err := a.DB.LoginFailureEvent.Query().
		Where(loginfailureevent.IP(staleFailureIP)).
		Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if staleFailures != 0 {
		t.Fatalf("stale download failures = %d, want 0", staleFailures)
	}
	if correct := request(router, ip, "correct"); correct.Code != http.StatusOK {
		t.Fatalf("correct password after ten failures = %d, want 200", correct.Code)
	}
	banStarted := time.Now()
	banned := request(router, ip, "wrong")
	if banned.Code != http.StatusTooManyRequests {
		t.Fatalf("eleventh failure status = %d, want 429", banned.Code)
	}
	ban, err := a.DB.IpBan.Get(context.Background(), "download:"+ip)
	if err != nil {
		t.Fatalf("persistent download ban: %v", err)
	}
	until, err := time.Parse(time.RFC3339Nano, ban.BannedUntil)
	if err != nil {
		t.Fatal(err)
	}
	duration := until.Sub(banStarted)
	if duration < 23*time.Hour || duration > 29*time.Hour+time.Second {
		t.Fatalf("download ban duration = %v, want 23h..29h", duration)
	}
	restartedRouter := httpx.New(a)
	if persisted := request(restartedRouter, ip, "correct"); persisted.Code != http.StatusTooManyRequests {
		t.Fatalf("persisted ban status = %d, want 429", persisted.Code)
	}
	if otherIP := request(restartedRouter, "203.0.113.21", "wrong"); otherIP.Code != http.StatusUnauthorized {
		t.Fatalf("independent IP status = %d, want 401", otherIP.Code)
	}
	failures, err := a.DB.LoginFailureEvent.Query().
		Where(loginfailureevent.IP("download:" + ip)).
		Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if failures != 11 {
		t.Fatalf("recorded failures = %d, want 11", failures)
	}
}

func TestAPIDownloadRemovesExpiredBanBeforeNextRequest(t *testing.T) {
	a := newTestApp(t)
	router := httpx.New(a)
	id := "00000000-0000-0000-0000-000000000126"
	insertProtectedShare(t, a, id, "encrypted-payload", futureTS(time.Hour), "correct")
	ip := "203.0.113.22"
	if _, err := a.DB.IpBan.Create().
		SetID("download:" + ip).
		SetBannedUntil(time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)).
		Save(context.Background()); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/"+id, strings.NewReader(downloadPasswordJSON("correct")))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":4321"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("request after ban expiry status = %d, want 200", w.Code)
	}
	banCount, err := a.DB.IpBan.Query().Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if banCount != 0 {
		t.Fatalf("expired ban rows = %d, want 0", banCount)
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

func TestAPIDownloadTwoSecondDelayAppliesToDeniedRequests(t *testing.T) {
	a := newTestApp(t)
	router := httpx.New(a)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/not-a-uuid", strings.NewReader(downloadPasswordJSON("wrong")))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	started := time.Now()
	router.ServeHTTP(w, req)
	if elapsed := time.Since(started); elapsed < 2*time.Second {
		t.Fatalf("denied download delay = %v, want at least 2s", elapsed)
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("denied download status = %d, want 401", w.Code)
	}
}

func TestAPIRequiresHTTPSWithoutCreatingSession(t *testing.T) {
	a, router := newRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/not-a-uuid", strings.NewReader(downloadPasswordJSON("wrong")))
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
