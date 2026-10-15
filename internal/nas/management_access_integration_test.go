package nas

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 使用生产 TLS listener/Management/SQLite，只有临时文件和 fixture qB/Emby。
func startAccessManagement(t *testing.T, f *fixture) (*Management, *http.Client, string) {
	t.Helper()
	raw, _ := accessTestVerifier(t)
	c := accessTestConfig(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c.Listen = l.Addr().String()
	l.Close()
	t.Setenv(c.PasswordHashEnv, raw)
	roots := accessTestCertificate(t, c, "nas.example.test", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), x509.ExtKeyUsageServerAuth)
	d := &Daemon{Engine: f.engine, Config: Config{Management: c, Subscriptions: []Subscription{{ID: "fixture", URL: "http://127.0.0.1:1/rss?token=synthetic-private"}}}}
	if _, err = NewManagement(context.Background(), d, "127.0.0.1:8787"); err == nil {
		t.Fatal("enabled authentication silently fell back to local handler")
	}
	l, m, err := OpenAuthenticatedManagement(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeManagement(ctx, l, m) }()
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "nas.example.test", MinVersion: tls.VersionTLS12}}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error("TLS shutdown", err)
			}
		case <-time.After(5 * time.Second):
			l.Close()
			t.Error("TLS management did not drain")
		}
	})
	return m, client, "https://" + l.Addr().String()
}

func requestAccessManagement(t *testing.T, client *http.Client, base, method, path string, payload any, cookie *http.Cookie, csrf string) (int, []byte, []*http.Cookie) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	r, err := http.NewRequest(method, base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	r.Host = "nas.example.test:8787"
	if method == "POST" {
		r.Header.Set("Origin", accessTestOrigin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Nonce", csrf)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	res, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("private response may be cached")
	}
	return res.StatusCode, data, res.Cookies()
}

func TestManagementAccessProductionTLSAndFrozenWorkflow(t *testing.T) {
	f := newFixture(t, "Example [02].mkv", 0)
	f.tick(1000)
	m, client, base := startAccessManagement(t, f)
	before := f.job()
	for _, path := range []string{"/", "/app.js", "/style.css", "/api/session"} {
		code, b, _ := requestAccessManagement(t, client, base, "GET", path, nil, nil, "")
		if code != 200 || bytes.Contains(b, []byte("synthetic-private")) || bytes.Contains(b, []byte(f.downloads)) || bytes.Contains(b, []byte(f.name)) {
			t.Fatal("anonymous bootstrap privacy", path, code)
		}
	}
	for _, path := range []string{"/api/jobs", "/api/jobs/" + before.ID, "/api/health", "/api/events?after=0", "/api/naming?job_id=" + before.ID, "/api/comparisons?id=" + before.ID} {
		code, b, _ := requestAccessManagement(t, client, base, "GET", path, nil, nil, "")
		if code != 401 || string(b) != "{\"error\":\"authentication_required\"}\n" {
			t.Fatal("anonymous API leaked", path, code, string(b))
		}
	}
	code, _, _ := requestAccessManagement(t, client, base, "POST", "/api/job-action", map[string]any{"id": before.ID, "version": before.Version, "action": "retry"}, nil, m.csrf)
	if code != 401 || f.job().Version != before.Version {
		t.Fatal("process nonce granted human authority")
	}
	code, b, cookies := requestAccessManagement(t, client, base, "POST", "/api/login", map[string]string{"username": "admin", "password": accessTestPassword}, nil, "")
	var session managementSessionDTO
	if code != 200 || json.Unmarshal(b, &session) != nil || len(cookies) != 1 {
		t.Fatal("production login", code)
	}
	cookie := cookies[0]
	code, b, _ = requestAccessManagement(t, client, base, "GET", "/api/health", nil, cookie, "")
	var health map[string]any
	if code != 200 || json.Unmarshal(b, &health) != nil || health["mode"] != "authenticated_https" || health["gateway"] != "unknown" {
		t.Fatal("authenticated health mode", code)
	}
	organization := BatchOrganization{Directory: "Human reviewed", DisplayName: "Example", Season: 3, Mode: "manual", Rows: []BatchRow{{Source: f.name, Episode: 2}}}
	previewInput := map[string]any{"id": before.ID, "version": before.Version, "organization": organization}
	code, b, _ = requestAccessManagement(t, client, base, "POST", "/api/batch-preview", previewInput, cookie, session.CSRF)
	var preview BatchPreview
	if code != 200 || json.Unmarshal(b, &preview) != nil || preview.Plan == nil {
		t.Fatal("authenticated full-batch preview", code, string(b))
	}
	confirm := map[string]any{"id": before.ID, "version": preview.JobVersion, "plan_version": preview.Plan.Version, "digest": preview.Digest, "organization": organization}
	// 已认证仍不能跳过本会话 CSRF；失败不创建批准或对比记录。
	code, _, _ = requestAccessManagement(t, client, base, "POST", "/api/batch-confirm", confirm, cookie, m.csrf)
	if code != 403 || f.job().Plan != nil {
		t.Fatal("process nonce bypassed session CSRF")
	}
	code, b, _ = requestAccessManagement(t, client, base, "POST", "/api/batch-confirm", confirm, cookie, session.CSRF)
	if code != 200 {
		t.Fatal("authenticated confirmation", code, string(b))
	}
	frozen := f.job()
	if frozen.Plan == nil || !frozen.Plan.Approved {
		t.Fatal("confirmation did not freeze approved intent")
	}
	code, _, _ = requestAccessManagement(t, client, base, "POST", "/api/batch-confirm", confirm, cookie, session.CSRF)
	if code != 200 || f.job().Version != frozen.Version {
		t.Fatal("identical confirmed replay lost idempotency")
	}
	confirm["digest"] = "tampered"
	code, _, _ = requestAccessManagement(t, client, base, "POST", "/api/batch-confirm", confirm, cookie, session.CSRF)
	if code != 409 || f.job().Version != frozen.Version {
		t.Fatal("authentication bypassed original plan binding")
	}
	confirm["digest"] = preview.Digest
	code, _, _ = requestAccessManagement(t, client, base, "POST", "/api/logout", map[string]any{}, cookie, session.CSRF)
	if code != 200 {
		t.Fatal("production logout")
	}
	code, _, _ = requestAccessManagement(t, client, base, "POST", "/api/batch-confirm", confirm, cookie, session.CSRF)
	if code != 401 || PlanDigest(f.job().Plan) != PlanDigest(frozen.Plan) {
		t.Fatal("logout replay changed frozen intent")
	}
	// 会话注销不撤销已批准意图；离线 fixture executor 仍恢复、硬链接并逐文件发现。
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	f.tick(1001)
	j := f.job()
	f.mu.Lock()
	stops := f.stops
	f.mu.Unlock()
	if j.Import != "imported" || j.Discovery != "confirmed" || j.Seed != "stopped" || stops != 1 {
		t.Fatal("session lifetime coupled to execution/seeding", j)
	}
	src, err := os.Stat(filepath.Join(f.downloads, f.name))
	if err != nil {
		t.Fatal(err)
	}
	dst, err := os.Stat(filepath.Join(f.library, j.Plan.Files[0].Target))
	if err != nil || !os.SameFile(src, dst) {
		t.Fatal("source retained hardlink missing", err)
	}
	t.Log("PASS synthetic trusted TLS login -> session CSRF -> complete batch preview/confirmation -> logout rejects replay -> retained-source hardlink and per-file Emby discovery; seed stop independent")
}

func TestManagementAccessLocalCompatibilityAndConfigLoad(t *testing.T) {
	f := newFixture(t, "Example [02].mkv", 0)
	m, server := managementFixture(t, f)
	res, err := server.Client().Get(server.URL + "/api/session")
	if err != nil {
		t.Fatal(err)
	}
	var session managementSessionDTO
	err = json.NewDecoder(res.Body).Decode(&session)
	res.Body.Close()
	if err != nil || session.Mode != "local_single_user" || !session.Authenticated || session.CSRF != m.csrf || session.ExpiresAt != 0 || len(res.Cookies()) != 0 {
		t.Fatal("old local session compatibility lost", err)
	}
	for _, listen := range []string{"0.0.0.0:8787", "192.168.1.9:8787", "[::]:8787"} {
		if l, err := ListenManagement(listen); err == nil {
			l.Close()
			t.Fatal("old unauthenticated listener broadened")
		}
	}
	c := Config{Database: filepath.Join(t.TempDir(), "state.db"), Downloads: f.downloads, Library: f.library, QBURL: "http://127.0.0.1:1", EmbyURL: "http://127.0.0.1:2", Management: accessTestConfig(t)}
	b, _ := json.Marshal(c)
	// 配置校验不需要真实 env/证书存在；只有显式 serve 才消耗它们。
	if _, err := LoadConfig(bytes.NewReader(b)); err != nil {
		t.Fatal("structural configuration validation read runtime files", err)
	}
	c.Events.TokenEnv = c.Management.PasswordHashEnv
	b, _ = json.Marshal(c)
	if _, err := LoadConfig(bytes.NewReader(b)); err == nil || err.Error() != "management_identity_reused" {
		t.Fatal("shared identity reference accepted", err)
	}
	// 原始 plaintext 传到真实 TLS listener 不得得到 API/session 内容。
	_, _, base := startAccessManagement(t, f)
	u, _ := url.Parse(base)
	plainClient := &http.Client{Timeout: 3 * time.Second}
	plain, err := plainClient.Get("http://" + u.Host + "/api/session")
	if err == nil {
		defer plain.Body.Close()
		body, _ := io.ReadAll(plain.Body)
		if plain.StatusCode == 200 || strings.Contains(string(body), "csrf") || strings.Contains(string(body), "authenticated") {
			t.Fatal("plain HTTP received management access")
		}
	}
}
