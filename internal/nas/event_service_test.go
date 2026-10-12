package nas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const eventTestToken = "fixture-only-token-012345678901234567890"

func eventServer(t *testing.T, s *Store) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	server.Config.Handler = EventHandler(s, eventTestToken, server.Listener.Addr().String())
	server.Start()
	t.Cleanup(server.Close)
	return server
}
func TestEventServiceAuthAndReplay(t *testing.T) {
	f := newFixture(t, "Example.S01E02.mkv", 0)
	f.tick(1000)
	server := eventServer(t, f.store)
	for _, tc := range []struct {
		name, method, auth, path, origin, host string
		status                                 int
	}{
		{"missing", "GET", "", "/v1/events?after=0", "", "", 401},
		{"nonce", "GET", "nonce", "/v1/events?after=0", "", "", 401},
		{"browser", "GET", "Bearer " + eventTestToken, "/v1/events?after=0", "http://evil.invalid", "", 401},
		{"host", "GET", "Bearer " + eventTestToken, "/v1/events?after=0", "", "evil.invalid", 401},
		{"write", "POST", "Bearer " + eventTestToken, "/v1/events?after=0", "", "", 405},
		{"cursor", "GET", "Bearer " + eventTestToken, "/v1/events?after=-1", "", "", 400},
		{"extra", "GET", "Bearer " + eventTestToken, "/v1/events?after=0&url=secret", "", "", 400},
		{"no management", "GET", "Bearer " + eventTestToken, "/api/jobs?after=0", "", "", 404},
		{"valid", "GET", "Bearer " + eventTestToken, "/v1/events?after=0", "", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := http.NewRequest(tc.method, server.URL+tc.path, nil)
			r.Header.Set("Authorization", tc.auth)
			r.Header.Set("Origin", tc.origin)
			if tc.host != "" {
				r.Host = tc.host
			}
			res, e := server.Client().Do(r)
			if e != nil {
				t.Fatal(e)
			}
			defer res.Body.Close()
			b, _ := io.ReadAll(res.Body)
			if res.StatusCode != tc.status {
				t.Fatal(res.StatusCode, string(b))
			}
			for _, secret := range []string{"magnet:", f.downloads, "untrusted secret", eventTestToken} {
				if strings.Contains(string(b), secret) {
					t.Fatal("unsafe payload")
				}
			}
		})
	}
	c := EventServiceConfig{Enabled: true, TokenEnv: "FIXTURE_NAS_EVENT_TOKEN", Listen: "127.0.0.1:0"}
	t.Setenv(c.TokenEnv, eventTestToken)
	l, _, e := OpenEventService(c, f.store)
	if e != nil {
		t.Fatal(e)
	}
	l.Close()
	for _, listen := range []string{"0.0.0.0:8788", "localhost:8788", "192.168.1.1:8788"} {
		c.Listen = listen
		if c.Validate() == nil {
			t.Fatal("unsafe listener")
		}
	}
	c.Listen = "127.0.0.1:0"
	t.Setenv(c.TokenEnv, "")
	if l, _, e := OpenEventService(c, f.store); e == nil {
		l.Close()
		t.Fatal("missing credential")
	}
}

// 两类名称仅是合成metadata，季号/目录由测试人员明确选择，绝不推断现实季度。
func notificationVertical(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t, "东京复仇者 三天战争篇 [02].mkv", -1)
	title := "东京复仇者 三天战争篇 [02]"
	rss := `<rss><channel><item><guid>ep2</guid><title>` + title + `</title><enclosure url="magnet:?xt=urn:btih:` + f.hash + `"/></item></channel></rss>`
	for i := 0; i < 2; i++ {
		if _, e := f.engine.IngestRSS(ctx, "fixture", strings.NewReader(rss), 0); e != nil {
			t.Fatal(e)
		}
	}
	f.tick(1000)
	if f.job().Import != "needs_confirmation" || f.adds != 1 {
		t.Fatal("mapping gate or duplicate add")
	}
	m, s := managementFixture(t, f)
	r := manualRule()
	r.Group = ""
	r.Aliases = []string{"东京复仇者 三天战争篇"}
	r.Season = 9
	r.Directory = "人工测试季09" // 合成选择，不声称现实季号。
	for _, request := range []struct {
		path  string
		value any
	}{
		{"rule-preview", map[string]any{"rule": r, "title": title, "source": f.name}},
		{"rules", r},
		{"rule-state", map[string]any{"id": r.ID, "revision": 1, "enabled": true, "epoch": 0}},
		{"job-action", map[string]any{"id": f.job().ID, "version": f.job().Version, "action": "replan"}},
	} {
		code, b := postManagement(t, m, s, request.path, request.value)
		if code != 200 {
			t.Fatal(request.path, code, string(b))
		}
	}
	f.tick(1001)
	if f.job().Import != "imported" || f.job().Discovery == "confirmed" || f.adds != 1 {
		t.Fatal("import/discovery separation")
	}
	f.discovered = true
	f.tick(1002)
	if f.job().Discovery != "confirmed" || f.stops != 1 {
		t.Fatal("ready and seed independent")
	}
	// 下一集沿用启用规则，无需逐集确认。
	f.hash = strings.Repeat("a", 40)
	f.name = "东京复仇者 三天战争篇 [03].mkv"
	os.WriteFile(filepath.Join(f.downloads, f.name), []byte("episode3"), 0600)
	id := addRelease(t, f, "fixture", "ep3", "东京复仇者 三天战争篇 [03]")
	f.tick(1003)
	j, e := f.store.Get(ctx, id)
	if e != nil || j.Import != "imported" || j.Plan.Decision.Season != 9 || j.Plan.Decision.Episode != 3 {
		t.Fatal(j, e)
	}
	return f
}
func TestNotificationDownloadMappingVertical(t *testing.T) {
	f := notificationVertical(t)
	events, e := f.store.Events(context.Background(), 0)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, v := range events {
		if v.Kind == "discovered" {
			found = true
		}
	}
	if !found {
		t.Fatal("no ready event")
	}
	for _, mode := range []string{"relative", "absolute"} {
		t.Run("ReZero-"+mode, func(t *testing.T) {
			p := packFixture(t, mode)
			p.store.SetRule(context.Background(), packRule().ID, 1, false)
			r := packRule()
			r.ID = "rezero"
			r.Aliases = []string{"Re:Zero 第四季"}
			r.Group = ""
			installRule(t, p.store, r)
			_, e := p.store.db.Exec(`DELETE FROM release_context`)
			if e != nil {
				t.Fatal(e)
			}
			addRelease(t, p, "fixture", "rezero", "Re:Zero 第四季 [01–11] [总第67–77]")
			p.tick(1000)
			if p.job().Import != "imported" || p.job().Discovery == "confirmed" || len(p.job().Plan.Decisions) != 11 {
				t.Fatal(p.job().BlockReason)
			}
			p.discovered = true
			p.tick(1001)
			if p.job().Discovery != "confirmed" {
				t.Fatal("pack not ready")
			}
		})
	}
}

// TestGatewayFakeSource 是跨仓fake入口；默认skip，不影响常规验证。不创建真实凭据或服务。
func TestGatewayFakeSource(t *testing.T) {
	dir := os.Getenv("NAS_GATEWAY_SMOKE_DIR")
	if dir == "" {
		t.Skip("explicit cross-repo fake smoke only")
	}
	f := notificationVertical(t)
	s := eventServer(t, f.store)
	b, _ := json.Marshal(map[string]string{"url": s.URL})
	if e := os.WriteFile(filepath.Join(dir, "ready.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(filepath.Join(dir, "done")); e == nil {
			fmt.Println("PASS NAS fake download/mapping -> authenticated replay -> gateway fixture")
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("gateway fake consumer did not finish")
}
