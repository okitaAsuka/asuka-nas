package nas

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
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
	var approvedIntent []byte
	var frozenDigest string
	// fixture 控制独立回环服务；不在生产 Management 上增加测试后门。
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		switch r.URL.Path {
		case "/expire":
			management.access.mu.Lock()
			clear(management.access.sessions)
			management.access.mu.Unlock()
		case "/assert-unconfirmed":
			entries, readErr := os.ReadDir(f.library)
			if readErr != nil || len(entries) != 0 || f.job().Plan != nil || len(comparisons(t, f)) != 0 {
				err = fmt.Errorf("unauthorized or automatic confirmation changed fixture")
			}
		case "/assert-confirmed":
			records := comparisons(t, f)
			if f.job().Plan == nil || !f.job().Plan.Approved || len(records) != 1 || records[0].Status != "confirmed_intent" {
				err = fmt.Errorf("explicit confirmation missing or duplicated")
			} else {
				approvedIntent, err = json.Marshal(records[0].Intent)
				frozenDigest = PlanDigest(f.job().Plan)
			}
		case "/execute":
			err = f.engine.Tick(context.Background(), time.Unix(1001, 0))
			if err == nil && (f.job().Import != "imported" || f.job().Discovery == "confirmed") {
				err = fmt.Errorf("refresh mistaken for per-file discovery")
			}
		case "/discover":
			f.mu.Lock()
			f.discovered = true
			f.mu.Unlock()
			err = f.engine.Tick(context.Background(), time.Unix(1002, 0))
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer control.Close()
	commandCtx, stopCommand := context.WithTimeout(context.Background(), 3*time.Minute)
	defer stopCommand()
	command := exec.CommandContext(commandCtx, "python3", "../../tests/https_auth_browser.py")
	command.Env = append(os.Environ(), "NAS_WEB_URL="+c.Origin, "NAS_FIXTURE_URL="+control.URL,
		"NAS_TEST_CA="+filepath.Join(filepath.Dir(c.TLSCertFile), "synthetic-ca.crt"))
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	j := f.job()
	records := comparisons(t, f)
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
	t.Log("PASS actual Chromium trusted TLS + production management/SQLite + manual batch + immutable intent + per-file discovery + retained-source seeding deadline")
}
