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

func TestAPIUploadRejectsPlaintextPasswordField(t *testing.T) {
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
	if w.Code != http.StatusBadRequest {
		t.Fatalf("plaintext password upload status = %d, want 400", w.Code)
	}
	count, err := a.DB.Share.Query().Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("plaintext password upload stored %d shares", count)
	}
}

func TestAPIUploadRejectsPlaintextPasswordFieldAfterBlob(t *testing.T) {
	a, router := newRouter(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"title": "api payload", "visibility": "public", "password_hash": testDownloadPasswordHash("download-password"),
		"encrypted": "1", "cipher_meta": testCipherMeta, "expiry_hours": "6",
	} {
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
	if err := form.WriteField("password", "download-password"); err != nil {
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
	if w.Code != http.StatusBadRequest {
		t.Fatalf("trailing plaintext password upload status = %d, want 400", w.Code)
	}
	count, err := a.DB.Share.Query().Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("trailing plaintext password upload stored %d shares", count)
	}
}

func TestAPIUploadRejectsServerSideEncryption(t *testing.T) {
	_, router := newRouter(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"title": "plain API payload", "visibility": "public", "password_hash": testDownloadPasswordHash("download-password"),
		"encrypted": "0", "cipher_meta": testCipherMeta, "expiry_hours": "6",
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
	if w.Code != http.StatusBadRequest {
		t.Fatalf("server-side encryption upload status = %d, want 400", w.Code)
	}
}

func TestAPIPrivateArchiveListReturnsOnlyActiveKeyMatches(t *testing.T) {
	a, router := newRouter(t)
	privateKey := "team-private-key"
	keyHash := auth.HMACKey(a.C.AppSecret, privateKey)
	seed := func(id, expiry string, matches bool) {
		t.Helper()
		insertProtectedShare(t, a, id, "encrypted-payload", expiry, "correct")
		update := a.DB.Share.UpdateOneID(id).
			SetVisibility("private").
			SetCipherMeta(testCipherMeta)
		if matches {
			update.SetPrivateKeyHash(keyHash)
		} else {
			update.SetPrivateKeyHash(auth.HMACKey(a.C.AppSecret, "other-key"))
		}
		if err := update.Exec(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	wantedID := "00000000-0000-0000-0000-000000000127"
	seed(wantedID, futureTS(time.Hour), true)
	seed("00000000-0000-0000-0000-000000000128", pastTS(time.Hour), true)
	seed("00000000-0000-0000-0000-000000000129", futureTS(time.Hour), false)
	publicID := "00000000-0000-0000-0000-000000000130"
	insertProtectedShare(t, a, publicID, "encrypted-payload", futureTS(time.Hour), "correct")
	if err := a.DB.Share.UpdateOneID(publicID).SetPrivateKeyHash(keyHash).SetCipherMeta(testCipherMeta).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/list", strings.NewReader(`{"private_key":"`+privateKey+`"}`))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("private archive list status = %d, body=%q", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("private archive list cache policy = %q", w.Header().Get("Cache-Control"))
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("private archive list created session cookie: %q", w.Header().Get("Set-Cookie"))
	}
	var response struct {
		Archives []struct {
			ID          string         `json:"id"`
			Title       string         `json:"title"`
			URL         string         `json:"url"`
			DownloadURL string         `json:"download_url"`
			CipherMeta  map[string]any `json:"cipher_meta"`
		} `json:"archives"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Archives) != 1 || response.Archives[0].ID != wantedID {
		t.Fatalf("private archive list = %+v, want only %s", response.Archives, wantedID)
	}
	archive := response.Archives[0]
	if archive.Title == "" || archive.URL != "/s/"+wantedID || archive.DownloadURL != "/api/v0/download/"+wantedID || archive.CipherMeta["cipher"] != "AES-256-GCM" {
		t.Fatalf("private archive response incomplete: %+v", archive)
	}
	for _, sensitive := range []string{privateKey, keyHash, "download-hash", a.C.BlobDir, "1.2.3.4"} {
		if strings.Contains(w.Body.String(), sensitive) {
			t.Fatalf("private archive response exposed sensitive value %q", sensitive)
		}
	}
	sessions, err := a.DB.Session.Query().Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("private archive list created %d session rows", sessions)
	}

	noMatch := httptest.NewRequest(http.MethodPost, "/api/v0/list", strings.NewReader("private_key=unknown-key"))
	noMatch.TLS = &tls.ConnectionState{}
	noMatch.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	noMatchResponse := httptest.NewRecorder()
	router.ServeHTTP(noMatchResponse, noMatch)
	if noMatchResponse.Code != http.StatusOK || strings.TrimSpace(noMatchResponse.Body.String()) != `{"archives":[]}` {
		t.Fatalf("unknown private key response = %d %q", noMatchResponse.Code, noMatchResponse.Body.String())
	}
}

func TestAPIPrivateArchiveListRejectsCredentialsOutsideStrictBody(t *testing.T) {
	_, router := newRouter(t)
	tests := []struct {
		name        string
		target      string
		contentType string
		body        string
		secure      bool
		want        int
	}{
		{name: "query", target: "/api/v0/list?private_key=secret", contentType: "application/json", body: `{}`, secure: true, want: http.StatusBadRequest},
		{name: "duplicate json", target: "/api/v0/list", contentType: "application/json", body: `{"private_key":"one","private_key":"two"}`, secure: true, want: http.StatusBadRequest},
		{name: "unknown json", target: "/api/v0/list", contentType: "application/json", body: `{"private_key":"one","extra":"two"}`, secure: true, want: http.StatusBadRequest},
		{name: "second json value", target: "/api/v0/list", contentType: "application/json", body: `{"private_key":"one"} {}`, secure: true, want: http.StatusBadRequest},
		{name: "duplicate form", target: "/api/v0/list", contentType: "application/x-www-form-urlencoded", body: `private_key=one&private_key=two`, secure: true, want: http.StatusBadRequest},
		{name: "unsupported content type", target: "/api/v0/list", contentType: "text/plain", body: `private_key=one`, secure: true, want: http.StatusBadRequest},
		{name: "insecure transport", target: "/api/v0/list", contentType: "application/json", body: `{"private_key":"one"}`, want: http.StatusUpgradeRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.target, strings.NewReader(tt.body))
			if tt.secure {
				req.TLS = &tls.ConnectionState{}
			}
			req.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d; body=%q", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestAPIClientUploadAndDownloadAcceptBrowserPasswordHash(t *testing.T) {
	a, router := newRouter(t)
	passwordHash := testDownloadPasswordHash("browser-only-password")
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
	if !ok || !auth.CheckPasswordHash(stored.DownloadPasswordHash, passwordHash) {
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
	passwordHash := url.QueryEscape(testDownloadPasswordHash("correct"))
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
	passwordHash := url.QueryEscape(testDownloadPasswordHash("correct"))
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
	req := httptest.NewRequest(http.MethodPost, "/api/v0/download/"+id, strings.NewReader("password_hash="+url.QueryEscape(testDownloadPasswordHash("correct"))))
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unsupported content type status = %d, want 401", w.Code)
	}
}

func TestAPIEncryptedUploadRejectsOversizedSourceWithoutStoredBlob(t *testing.T) {
	a := newTestApp(t)
	a.C.MaxUploadBytes = 1
	router := httpx.New(a)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"title": "large encrypted payload", "visibility": "public", "password_hash": testDownloadPasswordHash("download-password"),
		"encrypted": "1", "cipher_meta": testCipherMeta, "expiry_hours": "6",
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
		t.Fatalf("oversized encrypted upload status = %d, body=%q", w.Code, w.Body.String())
	}
	if count := countBlobs(t, a.C.BlobDir); count != 0 {
		t.Fatalf("oversized encrypted upload stored %d blobs", count)
	}
}

func TestAPIRejectsOversizedMetadataInsteadOfTruncatingIt(t *testing.T) {
	a, router := newRouter(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range map[string]string{
		"title": "large metadata", "visibility": "private", "password_hash": testDownloadPasswordHash("download-password"),
		"private_key": strings.Repeat("k", (64<<10)+1), "encrypted": "1", "cipher_meta": testCipherMeta, "expiry_hours": "6",
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
	first.Header.Set("Accept-Language", "ko-KR,ko;q=0.9,en;q=0.8")
	firstResponse := httptest.NewRecorder()
	router.ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first page status = %d", firstResponse.Code)
	}
	assertBodyContains(t, firstResponse.Body.String(), `<html lang="ko">`, ">업로드</h1>", "필수 암호화 비밀번호")
	if strings.Contains(firstResponse.Body.String(), ">Upload</h1>") {
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
	assertBodyContains(t, secondResponse.Body.String(), `<html lang="ko">`, ">업로드</h1>", "필수 암호화 비밀번호")
}

func TestUnsupportedChineseLanguageFallsBackToEnglish(t *testing.T) {
	a, router := newRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/upload", nil)
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assertBodyContains(t, w.Body.String(), `<html lang="en">`, ">Upload</h1>")
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("session cookie missing")
	}
	if err := a.DB.Session.UpdateOneID(cookies[0].Value).SetLanguage("zh").Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := httptest.NewRequest(http.MethodGet, "/upload", nil)
	next.AddCookie(cookies[0])
	response := httptest.NewRecorder()
	router.ServeHTTP(response, next)
	assertBodyContains(t, response.Body.String(), `<html lang="en">`, ">Upload</h1>")
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
