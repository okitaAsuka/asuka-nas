package nas

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// 生产 HTTPS/SQLite/文件执行 + Chromium；信任只可写入显式一次性测试账户。
func TestHTTPSManagementBrowserLoop(t *testing.T) {
	if os.Getenv("NAS_AUTH_BROWSER_TEST") != "1" {
		t.Skip("run make verify-auth in a disposable CI runner")
	}
	if os.Getenv("NAS_AUTH_DISPOSABLE_TRUST") != "1" {
		t.Fatal("explicit disposable Chromium trust environment required")
	}
	runAuthenticatedBrowserUI(t)
}

// 复用 UI owner 4cf76bf 的真实表单流程，只保留一个 fixture；独立 gate 避免重复运行。
func runAuthenticatedBrowserUI(t *testing.T) {
	t.Helper()
	f, _ := batchFixture(t)
	raw, _ := accessTestVerifier(t)
	c := accessTestConfig(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c.Listen = l.Addr().String()
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	c.Origin = "https://" + c.Listen
	t.Setenv(c.PasswordHashEnv, raw)
	accessTestCertificate(t, c, "127.0.0.1", time.Now().Add(-time.Hour), time.Now().Add(time.Hour), x509.ExtKeyUsageServerAuth)
	d := &Daemon{Engine: f.engine, Config: Config{Management: c}}
	l, management, err := OpenAuthenticatedManagement(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeManagement(ctx, l, management) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			l.Close()
			t.Error("HTTPS browser fixture did not drain")
		}
	})
	// fixture 控制独立回环服务；不在生产 Management 上增加测试后门。
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/expire" {
			management.access.mu.Lock()
			for key, session := range management.access.sessions {
				session.expires = time.Now().Add(-time.Second)
				management.access.sessions[key] = session
			}
			management.access.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == "GET" && r.URL.Path == "/state" {
			j := f.job()
			entries, err := os.ReadDir(f.library)
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"approved": j.Plan != nil && j.Plan.Approved, "comparisons": len(comparisons(t, f)), "library_entries": len(entries)})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer control.Close()
	commandCtx, stopCommand := context.WithTimeout(context.Background(), 3*time.Minute)
	defer stopCommand()
	command := exec.CommandContext(commandCtx, "python3", "../../tests/authenticated_browser.py")
	command.Env = append(os.Environ(), "NAS_AUTH_URL="+c.Origin, "NAS_AUTH_CONTROL="+control.URL,
		"NAS_AUTH_CA="+filepath.Join(filepath.Dir(c.TLSCertFile), "synthetic-ca.crt"), "NAS_AUTH_PASSWORD="+accessTestPassword)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	records := comparisons(t, f)
	j := f.job()
	if j.Plan == nil || !j.Plan.Approved || len(j.Plan.Files) != 3 || len(records) != 1 || records[0].Status != "confirmed_intent" {
		t.Fatal("explicit browser confirmation missing or duplicated")
	}
	approvedIntent, err := json.Marshal(records[0].Intent)
	if err != nil {
		t.Fatal(err)
	}
	frozenDigest := PlanDigest(j.Plan)
	f.tick(1001)
	if f.job().Import != "imported" || f.job().Discovery == "confirmed" {
		t.Fatal("refresh mistaken for per-file discovery")
	}
	f.engine.Emby = &batchPartialMedia{Media: f.engine.Emby, firstTarget: filepath.Join(f.library, j.Plan.Files[0].Target)}
	f.tick(1002)
	found := 0
	for _, file := range comparisons(t, f)[0].Files {
		if file.Video && file.Discovered {
			found++
		}
	}
	if found != 1 || f.job().Discovery == "confirmed" {
		t.Fatal("one discovered file incorrectly completed the batch")
	}
	f.engine.Emby = f.engine.Emby.(*batchPartialMedia).Media
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	f.tick(1003)
	j = f.job()
	records = comparisons(t, f)
	if j.Import != "imported" || j.Discovery != "confirmed" || len(records) != 1 || records[0].Status != "complete" {
		t.Fatal("trusted browser workflow did not complete")
	}
	finalIntent, err := json.Marshal(records[0].Intent)
	if err != nil || string(finalIntent) != string(approvedIntent) || PlanDigest(j.Plan) != frozenDigest {
		t.Fatal("authentication lifecycle changed frozen intent")
	}
	for _, file := range j.Plan.Files {
		src, err := os.Stat(filepath.Join(f.downloads, file.Source))
		if err != nil {
			t.Fatal(err)
		}
		dst, err := os.Stat(filepath.Join(f.library, file.Target))
		if err != nil || !os.SameFile(src, dst) {
			t.Fatal("source retained hardlink missing", err)
		}
	}
	f.tick(1000 + 7*24*60*60)
	f.mu.Lock()
	adds, stops := f.adds, f.stops
	f.mu.Unlock()
	if adds != 1 || stops != 1 || f.job().Seed != "stopped" {
		t.Fatal("download/seed lifecycle changed", adds, stops)
	}
	t.Log("PASS REAL AUTH EXECUTION: browser-approved immutable full batch -> retained-source hardlinks -> partial then all video fake Emby discovery -> independent 7-day stop; one download and one comparison")
}
