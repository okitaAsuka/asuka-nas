package nas

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIndependentControlProfileChurn(t *testing.T) {
	f := newFixture(t, "Example.S01E01.mkv", 0)
	ctx := context.Background()
	for round := 0; round < 2; round++ {
		ps := []Subscription{}
		for i := 0; i < 100; i++ {
			ps = append(ps, Subscription{ID: fmt.Sprintf("p%d-%03d", round, i)})
		}
		if err := f.store.InitControl(ctx, ps); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	f.store.db.QueryRow("SELECT count(*) FROM managed_subscriptions").Scan(&count)
	visible, err := f.store.ManagedSubscriptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current := 0
	for _, s := range visible {
		if strings.HasPrefix(s.Profile, "p1-") {
			current++
		}
	}
	t.Logf("persisted=%d visible=%d current-config-visible=%d/100", count, len(visible), current)
	if count != 200 || len(visible) != 200 || current != 100 {
		t.Fatal("history truncated")
	}
	var hits atomic.Int32
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, "<rss><channel></channel></rss>")
	}))
	defer feed.Close()
	ps := []Subscription{}
	for i := 0; i < 100; i++ {
		ps = append(ps, Subscription{ID: fmt.Sprintf("p1-%03d", i), URL: feed.URL})
	}
	d := &Daemon{Managed: true, Engine: f.engine, Config: Config{Subscriptions: ps, PollSeconds: 60}, Source: NewSourceClient(time.Second, 2<<20, 8<<20, 100)}
	if err := d.PollSources(ctx, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	t.Logf("actual local feed requests=%d; expected 100 current enabled sources", hits.Load())
	if hits.Load() != 100 {
		t.Fatal("current enabled sources were skipped")
	}

}
func TestIndependentHTTPNegativeMatrix(t *testing.T) {
	f := newFixture(t, "Example [02].mkv", 0)
	m, s := managementFixture(t, f)
	for _, tc := range []struct {
		method, path, origin, fetch string
		want                        int
	}{
		{"OPTIONS", "/api/job-action", s.URL, "", 405}, {"PUT", "/api/subscriptions", s.URL, "", 405},
		{"GET", "/api/session", "null", "", 403}, {"GET", "/api/session", "", "cross-site", 403},
		{"GET", "/../config.json", "", "", 404}, {"GET", "/api/job-action", "", "", 404},
	} {
		r := httptest.NewRequest(tc.method, s.URL+tc.path, nil)
		r.Host = m.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.fetch)
		w := httptest.NewRecorder()
		m.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("CORS")
		}
	}
	f.tick(1000)
	j := f.job()
	code, b := postManagement(t, m, s, "job-action", map[string]any{"id": j.ID, "version": j.Version, "action": "preview", "source": f.name, "target": "Example/Season 01/Example - S01E02.mkv"})
	if code != 200 {
		t.Fatal(code, string(b))
	}
	j = f.job()
	code, _ = postManagement(t, m, s, "job-action", map[string]any{"id": j.ID, "version": j.Version, "action": "confirm", "plan_version": j.Plan.Version, "digest": "wrong"})
	if code != 409 || f.job().Plan.Approved {
		t.Fatal("bad digest approved")
	}
	code, _ = postManagement(t, m, s, "subscriptions", map[string]any{"id": "unsafe", "profile": "fixture", "name": "unsafe", "url": "http://127.0.0.1:9", "enabled": true})
	if code != 409 {
		t.Fatal("arbitrary URL accepted")
	}
	t.Log("method/CORS/null origin/cross-site/static path/wrong digest/unknown URL field rejected")
}
