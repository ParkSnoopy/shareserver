package internaltest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestContainerGateway exercises the shipped supervisor and policy with real binaries.
// It owns ports 8080, 8081 and 9090; run it without another local stack listening.
func TestContainerGateway(t *testing.T) {
	anubis := os.Getenv("ANUBIS_BINARY")
	server := os.Getenv("SHARESERVER_BINARY")
	if anubis == "" || server == "" {
		t.Skip("set ANUBIS_BINARY and SHARESERVER_BINARY to run the real gateway integration")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, source)
	dockerfile, err := source.ReadFile("Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	anubisCopy := regexp.MustCompile(`(?m)^COPY --from=\S+ /ko-app/anubis (/\S+)$`).FindSubmatch(dockerfile)
	if len(anubisCopy) != 2 {
		t.Fatal("Dockerfile must install the packaged Anubis executable")
	}
	for _, mode := range []string{"direct", "proxy", "railway", "railway-direct", "custom-key"} {
		t.Run(mode, func(t *testing.T) {
			direct := mode == "direct" || mode == "railway-direct"
			containerRoot := t.TempDir()
			containerFiles, err := os.OpenRoot(containerRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestResource(t, containerFiles)
			dir := filepath.Join(containerRoot, "app")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			files, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestResource(t, files)
			for _, name := range []string{"entrypoint.sh", "anubis-policy.yaml"} {
				data, err := source.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				if err := files.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			anubisName := strings.TrimPrefix(string(anubisCopy[1]), "/")
			anubisPath := filepath.Join(containerRoot, anubisName)
			if err := containerFiles.MkdirAll(filepath.Dir(anubisName), 0700); err != nil {
				t.Fatal(err)
			}
			if err := containerFiles.Symlink(anubis, anubisName); err != nil {
				t.Fatal(err)
			}
			for name, target := range map[string]string{"shareserver": server, "web": filepath.Join(root, "web")} {
				if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
			// A legacy dotenv address must neither expose the backend nor be rewritten.
			const dotenv = "DEBUG=true\nADDR=0.0.0.0:8080\n"
			if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(dotenv), 0600); err != nil {
				t.Fatal(err)
			}
			log, err := files.OpenFile("process.log", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestResource(t, log)
			t.Setenv("PATH", filepath.Dir(anubisPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("DB_PATH", filepath.Join(dir, "test.db"))
			t.Setenv("BLOB_DIR", filepath.Join(dir, "blobs"))
			t.Setenv("DEBUG", "false")
			t.Setenv("ADMIN_PASSWORD", "test-only-admin-not-a-deployment-credential")
			t.Setenv("APP_SECRET", "")
			for _, key := range []string{"HS512_SECRET", "ED25519_PRIVATE_KEY_HEX", "ED25519_PRIVATE_KEY_HEX_FILE"} {
				t.Setenv(key, "")
			}
			if mode == "custom-key" {
				t.Setenv("HS512_SECRET", rand.Text())
			}
			t.Setenv("ANUBIS_BIND", "127.0.0.1:8080")
			t.Setenv("ANUBIS_USE_REMOTE_ADDRESS", strconv.FormatBool(direct))
			t.Setenv("RAILWAY_ENVIRONMENT_ID", "")
			if mode == "railway" || mode == "railway-direct" {
				t.Setenv("RAILWAY_ENVIRONMENT_ID", "test-only-environment")
			}
			if mode == "direct" || mode == "railway" {
				t.Setenv("ANUBIS_USE_REMOTE_ADDRESS", "")
			}
			difficulty := 2
			if mode == "direct" {
				difficulty = 4
				t.Setenv("ANUBIS_DIFFICULTY", "") // Exercise the supervisor default.
			} else {
				t.Setenv("ANUBIS_DIFFICULTY", strconv.Itoa(difficulty))
			}
			cmd := exec.Command("bash", "./entrypoint.sh")
			cmd.Dir = dir
			cmd.Stdout, cmd.Stderr = log, log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			stop := func() {
				t.Helper()
				_ = cmd.Process.Signal(os.Interrupt)
				select {
				case err := <-done:
					if err != nil {
						t.Errorf("supervisor shutdown: %v", err)
					}
				case <-time.After(10 * time.Second):
					_ = cmd.Process.Kill()
					t.Error("supervisor did not stop its children")
				}
				for _, address := range []string{"127.0.0.1:8080", "127.0.0.1:8081", "127.0.0.1:9090"} {
					conn, err := net.DialTimeout("tcp", address, time.Second)
					if err == nil {
						closeTestResource(t, conn)
						t.Errorf("listener survived supervisor shutdown: %s", address)
					}
				}
			}
			t.Cleanup(func() { stop() })
			client := &http.Client{Timeout: 2 * time.Second}
			ready := false
			for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
				resp, err := client.Get("http://127.0.0.1:8081/api/")
				if err == nil {
					closeTestResource(t, resp.Body)
					health, err := client.Get("http://127.0.0.1:9090/healthz")
					if err == nil {
						closeTestResource(t, health.Body)
						ready = resp.StatusCode == 200 && health.StatusCode == 200
					}
				}
				if ready {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !ready {
				data, _ := files.ReadFile("process.log")
				t.Fatalf("gateway not ready: %s", data)
			}
			if info, err := os.Stat(filepath.Join(dir, "test.db.secret")); err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("production gateway did not persist an owner-only application secret")
			}
			upstream, _ := url.Parse("http://127.0.0.1:8080")
			proxy := httputil.NewSingleHostReverseProxy(upstream)
			if mode == "railway" {
				// Each connection uses a different ingress peer for the same browser.
				var connections atomic.Uint32
				transport := &http.Transport{DisableKeepAlives: true}
				transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
					ip := net.IPv4(127, 0, 0, byte(2+connections.Add(1)%2))
					dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: ip}}
					return dialer.DialContext(ctx, network, address)
				}
				proxy.Transport = transport
				defer transport.CloseIdleConnections()
			}
			var clientIP atomic.Value
			clientIP.Store("198.51.100.42")
			edge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Header.Set("X-Real-IP", clientIP.Load().(string))
				r.Header.Set("X-Forwarded-For", clientIP.Load().(string))
				r.Header.Set("X-Forwarded-Proto", "https")
				proxy.ServeHTTP(w, r)
			}))
			defer edge.Close()
			client = edge.Client()
			client.Timeout = 5 * time.Second
			request := func(method, path, agent, body string) (int, string) {
				t.Helper()
				req, err := http.NewRequest(method, edge.URL+path, strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("User-Agent", agent)
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer closeTestResource(t, resp.Body)
				data, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				return resp.StatusCode, string(data)
			}
			for _, agent := range []string{"", "curl/8", "Mozilla/5.0", "Googlebot", "GPTBot"} {
				for _, path := range []string{"/", "/upload", "/api", "/api/", "/api/?q=v0/list", "/s/nope", "/admin", "/admin/login", "/static/css/app.css", "/static/js/upload.js", "/download-sw.js", "/robots.txt", "/unknown"} {
					status, body := request("GET", path, agent, "")
					if status != 200 || !strings.Contains(body, `id="anubis_challenge"`) {
						t.Fatalf("%s %q: expected challenge, got %d", path, agent, status)
					}
				}
			}
			for path, status := range map[string]int{"/api/v0/list": 400, "/api/v0/upload": 415, "/api/v0/download/nope": 401} {
				if direct {
					status = 426 // Spoofed HTTPS claims cannot authorize direct-mode requests.
				}
				got, body := request("POST", path, "curl/8", "{}")
				if got != status || strings.Contains(body, `id="anubis_challenge"`) {
					t.Fatalf("API bypass %s: got %d %q, want %d", path, got, body, status)
				}
			}
			if !direct {
				status, body := request("POST", "/api/v0/list", "curl/8", `{"private_key":"gateway-test-only"}`)
				if status != 200 || !strings.Contains(body, `"archives":[]`) {
					t.Fatalf("valid sessionless API request: %d %s", status, body)
				}
			}
			if status, body := request("POST", "/api/", "curl/8", "{}"); status != 200 || !strings.Contains(body, `id="anubis_challenge"`) {
				t.Fatal("the API HTML root must be challenged for POST too")
			}
			if status, _ := request("GET", "/.within.website/x/cmd/anubis/static/js/main.mjs", "curl/8", ""); status != 200 {
				t.Fatal("challenge JavaScript must remain accessible")
			}
			client.Jar, _ = cookiejar.New(nil)
			_, body := request("GET", "/api/", "Mozilla/5.0", "")
			match := regexp.MustCompile(`(?s)<script id="anubis_challenge"[^>]*>(.*?)</script>`).FindStringSubmatch(body)
			if len(match) != 2 {
				t.Fatal("missing challenge data")
			}
			var challenge struct {
				Rules     struct{ Difficulty int }
				Challenge struct {
					ID, RandomData string
					Metadata       map[string]string
				}
			}
			if err := json.Unmarshal([]byte(match[1]), &challenge); err != nil {
				t.Fatal(err)
			}
			if challenge.Rules.Difficulty != difficulty {
				t.Fatalf("difficulty = %d, want %d", challenge.Rules.Difficulty, difficulty)
			}
			ip := "127.0.0.1"
			if !direct {
				ip = "198.51.100.42"
			}
			if got := challenge.Challenge.Metadata["X-Real-Ip"]; got != ip {
				t.Fatalf("challenge client IP = %q, want %q", got, ip)
			}
			query := url.Values{"id": {challenge.Challenge.ID}, "redir": {edge.URL + "/api/"}, "elapsedTime": {"100"}}
			for nonce := 0; ; nonce++ {
				sum := sha256.Sum256([]byte(challenge.Challenge.RandomData + strconv.Itoa(nonce)))
				response := hex.EncodeToString(sum[:])
				if strings.HasPrefix(response, strings.Repeat("0", difficulty)) {
					query.Set("nonce", strconv.Itoa(nonce))
					query.Set("response", response)
					break
				}
				if nonce == 1_000_000 {
					t.Fatal("proof-of-work fixture exceeded search bound")
				}
			}
			status, body := request("GET", "/.within.website/x/cmd/anubis/api/pass-challenge?"+query.Encode(), "Mozilla/5.0", "")
			if status != 200 || !strings.Contains(body, `class="site-frame"`) || strings.Contains(body, `id="anubis_challenge"`) {
				t.Fatalf("solved challenge did not reach application: %d %s", status, body)
			}
			if status, body := request("GET", "/static/css/app.css", "Mozilla/5.0", ""); status != 200 || strings.Contains(body, `id="anubis_challenge"`) {
				t.Fatal("passed cookie did not authorize application CSS")
			}
			secret, err := files.ReadFile("test.db.secret")
			if err != nil {
				t.Fatal(err)
			}
			stop()
			cmd = exec.Command("bash", "./entrypoint.sh")
			cmd.Dir = dir
			cmd.Stdout, cmd.Stderr = log, log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done = make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			ready = false
			for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
				resp, err := http.Get("http://127.0.0.1:8081/api/")
				if err == nil {
					closeTestResource(t, resp.Body)
					health, err := http.Get("http://127.0.0.1:9090/healthz")
					if err == nil {
						closeTestResource(t, health.Body)
						ready = resp.StatusCode == 200 && health.StatusCode == 200
					}
				}
				if ready {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !ready {
				t.Fatal("gateway did not restart")
			}
			stored, err := files.ReadFile("test.db.secret")
			if err != nil || string(stored) != string(secret) {
				t.Fatal("application secret changed across supervisor restart")
			}
			if status, body := request("GET", "/api/", "Mozilla/5.0", ""); status != 200 || strings.Contains(body, `id="anubis_challenge"`) {
				t.Fatalf("solved challenge after restart: HTTP %d, rechallenged=%t", status, strings.Contains(body, `id="anubis_challenge"`))
			}
			if !direct {
				clientIP.Store("198.51.100.43")
				if _, body := request("GET", "/api/", "Mozilla/5.0", ""); !strings.Contains(body, `id="anubis_challenge"`) {
					t.Fatal("a different client IP must not inherit the solved cookie")
				}
			}
			data, err := files.ReadFile(".env")
			if err != nil || string(data) != dotenv {
				t.Fatal("supervisor modified existing dotenv configuration")
			}
			data, err = files.ReadFile("process.log")
			if err != nil || strings.Contains(string(data), string(secret)) || strings.Contains(string(data), "generating random key") {
				t.Fatal("gateway logged its secret or used an ephemeral signing key")
			}
		})
	}
	for _, value := range []string{"0", "65", "-1", "bad", "1.5", "01"} {
		t.Run("invalid-difficulty-"+value, func(t *testing.T) {
			t.Setenv("ANUBIS_DIFFICULTY", value)
			cmd := exec.Command("bash", "./entrypoint.sh")
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "ANUBIS_DIFFICULTY must be") {
				t.Fatalf("invalid difficulty was not rejected: %v %s", err, output)
			}
		})
	}
}
