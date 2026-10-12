package nas

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// 浏览器验收显式 opt-in；只运行本地 HTTP 和临时媒体文件，不在产品中暴露 fixture 控制。
func TestIndependentBrowserPreviewOwnership(t *testing.T) {
	if os.Getenv("NAS_BROWSER_TEST") != "1" {
		t.Skip("run make verify-ui with Python Playwright and Chromium")
	}
	ctx := context.Background()
	f := newFixture(t, "Example [02].mkv", -1)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		fmt.Fprintf(w, `<rss><channel><item><guid>%s</guid><title>Example [%s] &lt;img src=x onerror=window.PWNED=2&gt;</title><enclosure url="magnet:?xt=urn:btih:%s"/></item></channel></rss>`, f.hash, map[bool]string{true: "03", false: "02"}[f.hash != fixtureHash], f.hash)
	}))
	defer feed.Close()
	d := &Daemon{Engine: f.engine, Config: Config{Subscriptions: []Subscription{{ID: "fixture", URL: feed.URL + "/rss?token=hidden-secret"}}, PollSeconds: 60, SeedDays: 0}, Source: NewSourceClient(time.Second, 2<<20, 8<<20, 100)}
	web := httptest.NewUnstartedServer(nil)
	m, err := NewManagement(ctx, d, web.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	web.Config.Handler = m
	web.Start()
	defer web.Close()
	pack := packFixture(t, "relative")
	pack.engine.Files = &faultFiles{FileOperations: pack.files, at: 7, permission: true}
	if err = pack.engine.Tick(ctx, time.Unix(1000, 0)); err == nil {
		t.Fatal("missing partial failure")
	}
	packDigest := PlanDigest(pack.job().Plan)
	// 真实关闭并重开 SQLite，浏览器看到的是持久恢复的中间态。
	pack.reopen()
	_, packWeb := managementFixture(t, pack)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		switch r.URL.Path {
		case "/history":
			profiles := []Subscription{}
			for i := 0; i < 100; i++ {
				profiles = append(profiles, Subscription{ID: fmt.Sprintf("aaa-%03d", i)})
			}
			err = f.store.InitControl(ctx, profiles)
			if err == nil {
				err = f.store.InitControl(ctx, d.Config.Subscriptions)
			}
		case "/poll":
			err = d.PollSources(ctx, time.Unix(1000, 0))
		case "/tick":
			err = f.engine.Tick(ctx, time.Unix(1001, 0))
		case "/discover":
			f.mu.Lock()
			f.discovered = true
			f.mu.Unlock()
			err = f.engine.Tick(ctx, time.Unix(1002, 0))
		case "/next":
			f.mu.Lock()
			f.hash = "1123456789abcdef0123456789abcdef01234567"
			f.name = "Example [03].mkv"
			err = os.WriteFile(filepath.Join(f.downloads, f.name), []byte("next episode"), 0600)
			f.mu.Unlock()
			if err == nil {
				err = d.PollSources(ctx, time.Unix(1061, 0))
			}
			if err == nil {
				err = f.engine.Tick(ctx, time.Unix(1062, 0))
			}
		case "/pack-recover":
			err = pack.engine.Tick(ctx, time.Unix(1001, 0))
		case "/pack-discover":
			pack.mu.Lock()
			pack.discovered = true
			pack.mu.Unlock()
			err = pack.engine.Tick(ctx, time.Unix(1002, 0))
		default:
			w.WriteHeader(404)
			return
		}
		if err != nil {
			t.Errorf("fixture control: %v", err)
			w.WriteHeader(500)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer control.Close()
	command := exec.Command("python3", "../../tests/independent_browser.py")
	command.Env = append(os.Environ(), "NAS_WEB_URL="+web.URL, "NAS_PACK_URL="+packWeb.URL, "NAS_FIXTURE_URL="+control.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	ids, err := f.store.IDs(ctx)
	if err != nil || len(ids) != 2 {
		t.Fatal(ids, err)
	}
	for _, id := range ids {
		j, err := f.store.Get(ctx, id)
		if err != nil || j.Import != "imported" || j.Discovery != "confirmed" {
			t.Fatal(j, err)
		}
		for _, p := range j.Plan.Files {
			src, e := os.Stat(filepath.Join(f.downloads, p.Source))
			if e != nil {
				t.Fatal(e)
			}
			dst, e := os.Stat(filepath.Join(f.library, p.Target))
			if e != nil || !os.SameFile(src, dst) {
				t.Fatal("not hardlink", e)
			}
		}
	}
	if f.adds != 2 {
		t.Fatal("redownload", f.adds)
	}
	if j := pack.job(); j.Import != "imported" || j.Discovery != "confirmed" || PlanDigest(j.Plan) != packDigest {
		t.Fatal("pack recovery changed plan")
	}
	events, err := f.store.Events(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, event := range events {
		if event.Kind == "discovered" {
			seen[event.JobID]++
		}
	}
	for _, id := range ids {
		if seen[id] != 1 {
			t.Fatal("final event missing or duplicated", seen)
		}
	}
}
