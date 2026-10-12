package nas

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func managementFixture(t *testing.T, f *fixture) (*Management, *httptest.Server) {
	t.Helper()
	d := &Daemon{Engine: f.engine, Config: Config{Subscriptions: []Subscription{{ID: "fixture", URL: "http://127.0.0.1:1/rss?token=hidden-secret"}}}}
	server := httptest.NewUnstartedServer(nil)
	m, err := NewManagement(context.Background(), d, server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = m
	server.Start()
	t.Cleanup(server.Close)
	return m, server
}
func postManagement(t *testing.T, m *Management, server *httptest.Server, path string, value any) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(value)
	r, _ := http.NewRequest("POST", server.URL+"/api/"+path, bytes.NewReader(b))
	r.Header.Set("Origin", server.URL)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Nonce", m.csrf)
	res, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, body
}
func TestManagementSecurityBoundary(t *testing.T) {
	f := newFixture(t, "Example.S01E01.mkv", 0)
	m, s := managementFixture(t, f)
	for _, address := range []string{"0.0.0.0:8787", "[::]:8787", "192.168.1.2:8787", "localhost:8787"} {
		if l, e := ListenManagement(address); e == nil {
			l.Close()
			t.Fatal("unsafe bind", address)
		}
	}
	for _, tc := range []struct{ name, origin, host, csrf, content string }{{"origin", "http://evil.test", m.host, m.csrf, "application/json"}, {"host", s.URL, "evil.test", m.csrf, "application/json"}, {"missing origin", "", m.host, m.csrf, "application/json"}, {"missing csrf", s.URL, m.host, "", "application/json"}, {"form", s.URL, m.host, m.csrf, "application/x-www-form-urlencoded"}} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", s.URL+"/api/job-action", strings.NewReader(`{}`))
			r.Host = tc.host
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-CSRF-Nonce", tc.csrf)
			r.Header.Set("Content-Type", tc.content)
			w := httptest.NewRecorder()
			m.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatal(w.Code)
			}
		})
	}
	for _, path := range []string{"subscriptions", "jobs", "health", "events", "source-events"} {
		res, e := s.Client().Get(s.URL + "/api/" + path)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || strings.Contains(string(b), "hidden-secret") || strings.Contains(string(b), "http://127.0.0.1:1") || strings.Contains(string(b), "magnet:?") {
			t.Fatal(path, res.StatusCode, string(b))
		}
		if res.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("cache")
		}
	}
	code, _ := postManagement(t, m, s, "subscriptions", map[string]any{"id": "evil", "name": "evil", "profile": "http://127.0.0.1:9/admin", "enabled": true})
	if code != 409 {
		t.Fatal("SSRF reference accepted")
	}
	code, _ = postManagement(t, m, s, "rules", map[string]any{"bad": strings.Repeat("x", 70000)})
	if code != 409 {
		t.Fatal("oversize")
	}
}
func TestManagementSubscriptionLifecycleAndRetry(t *testing.T) {
	f := newFixture(t, "Example.S01E01.mkv", -1)
	m, s := managementFixture(t, f)
	ctx := context.Background()
	v := ManagedSubscription{ID: "new", Profile: "fixture", Name: "<img src=x onerror=alert(1)>", Enabled: true}
	code, b := postManagement(t, m, s, "subscriptions", v)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	v.Version = 1
	v.Enabled = false
	code, b = postManagement(t, m, s, "subscriptions", v)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, _ = postManagement(t, m, s, "subscriptions", v)
	if code != 409 {
		t.Fatal("stale edit accepted")
	}
	if _, err := f.store.Ingest(ctx, "new", "guid", fixtureHash, "magnet:?xt=urn:btih:"+fixtureHash, 0); err != nil {
		t.Fatal(err)
	}
	// 重启初始化不能再次启用被停用的配置订阅。
	config := ManagedSubscription{ID: "fixture", Profile: "fixture", Name: "fixture", Enabled: false, Version: 1}
	if err := f.store.PutSubscription(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := f.store.InitControl(ctx, m.Daemon.Config.Subscriptions); err != nil {
		t.Fatal(err)
	}
	subs, err := f.store.ManagedSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range subs {
		if v.Enabled {
			t.Fatal("disabled changed after init")
		}
	}
	if err = m.Daemon.PollSources(ctx, time.Now()); err != nil {
		t.Fatal("disabled source fetched", err)
	}
}
func TestManagementFrozenRetryAndConfirmationCAS(t *testing.T) {
	f := newFixture(t, "Example [02].mkv", 0)
	f.tick(1000)
	m, s := managementFixture(t, f)
	j := f.job()
	code, b := postManagement(t, m, s, "job-action", map[string]any{"id": j.ID, "version": j.Version, "action": "preview", "source": f.name, "target": "Example/Season 01/Example - S01E02.mkv"})
	if code != 200 {
		t.Fatal(code, string(b))
	}
	j = f.job()
	payload := map[string]any{"id": j.ID, "version": j.Version, "action": "confirm", "plan_version": j.Plan.Version, "digest": PlanDigest(j.Plan)}
	code, b = postManagement(t, m, s, "job-action", payload)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, _ = postManagement(t, m, s, "job-action", payload)
	if code != 409 {
		t.Fatal("duplicate confirmation accepted")
	}
	f.tick(1001)
	j = f.job()
	digest := PlanDigest(j.Plan)
	adds := f.adds
	code, b = postManagement(t, m, s, "job-action", map[string]any{"id": j.ID, "version": j.Version, "action": "retry"})
	if code != 200 {
		t.Fatal(code, string(b))
	}
	f.tick(1002)
	j = f.job()
	if f.adds != adds || PlanDigest(j.Plan) != digest {
		t.Fatal("retry changed frozen plan or downloaded again")
	}
	code, _ = postManagement(t, m, s, "job-action", map[string]any{"id": j.ID, "version": j.Version, "action": "replan"})
	if code != 409 {
		t.Fatal("frozen replan accepted")
	}
}
func TestManagementRuleDraftPreviewEnableAudit(t *testing.T) {
	f := newFixture(t, "Example [02].mkv", -1)
	m, s := managementFixture(t, f)
	r := manualRule()
	r.Subscription = "fixture"
	r.Aliases = []string{"Example"}
	r.Group = ""
	code, b := postManagement(t, m, s, "rule-preview", map[string]any{"rule": r, "title": "Example [02]", "source": "Example [02].mkv"})
	if code != 200 {
		t.Fatal(code, string(b))
	}
	rules, _ := f.store.RuleViews(context.Background())
	if len(rules) != 0 {
		t.Fatal("preview persisted")
	}
	code, b = postManagement(t, m, s, "rules", r)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, b = postManagement(t, m, s, "rule-state", map[string]any{"id": r.ID, "revision": r.Revision, "enabled": true, "epoch": 0})
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, _ = postManagement(t, m, s, "rule-state", map[string]any{"id": r.ID, "revision": r.Revision, "enabled": false, "epoch": 0})
	if code != 409 {
		t.Fatal("stale rule operation")
	}
	events, err := f.store.Audit(context.Background())
	if err != nil || len(events) != 2 {
		t.Fatal(events, err)
	}
}

func TestManagementShutdownWaitsForHandlers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l, err := ListenManagement("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); <-release })
	go func() { finished <- ServeManagement(ctx, l, handler) }()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		res, e := http.Get("http://" + l.Addr().String())
		if e == nil {
			res.Body.Close()
		}
	}()
	<-entered
	cancel()
	select {
	case <-finished:
		t.Fatal("server returned before active handler stopped")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case err = <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown stuck")
	}
	<-clientDone
}
func TestManagementConcurrentWritesAreBounded(t *testing.T) {
	f := newFixture(t, "Example.S01E01.mkv", 0)
	m, s := managementFixture(t, f)
	m.writes <- struct{}{}
	defer func() { <-m.writes }()
	code, b := postManagement(t, m, s, "job-action", map[string]any{})
	if code != 429 || !strings.Contains(string(b), "operation_busy") {
		t.Fatal(code, string(b))
	}
}
