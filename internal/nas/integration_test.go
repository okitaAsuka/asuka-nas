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
	"sync"
	"testing"
	"time"
)

const fixtureHash = "0123456789abcdef0123456789abcdef01234567"

// fixture 所有 HTTP 只在回环 fake 服务，媒体文件只在 t.TempDir。
type fixture struct {
	hash                         string
	multipartAdds                int
	wrongRoot                    bool
	t                            *testing.T
	mu                           sync.Mutex
	root, downloads, library, db string
	name                         string
	completed                    int64
	discovered                   bool
	failRefresh, failFiles       bool
	adds, stops, refreshes       int
	server                       *httptest.Server
	store                        *Store
	files                        *Files
	engine                       *Engine
}

func newFixture(t *testing.T, name string, days int) *fixture {
	t.Helper()
	f := &fixture{t: t, root: t.TempDir(), name: name, completed: 1000, hash: fixtureHash}
	f.downloads = filepath.Join(f.root, "downloads")
	f.library = filepath.Join(f.root, "library")
	f.db = filepath.Join(f.root, "state.db")
	for _, p := range []string{f.downloads, f.library} {
		if e := os.Mkdir(p, 0700); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(f.downloads, name), []byte("video fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	var e error
	f.files, e = OpenFiles(f.downloads, f.library)
	if e != nil {
		t.Fatal(e)
	}
	f.reopen()
	if days >= 0 {
		ids, e := f.engine.IngestRSS(context.Background(), "fixture", strings.NewReader(f.rss()), days)
		if e != nil || len(ids) != 1 {
			t.Fatal(ids, e)
		}
	}
	t.Cleanup(func() { f.server.Close(); f.files.Close(); f.store.Close() })
	return f
}
func (f *fixture) rss() string {
	return `<rss><channel><item><guid>one</guid><title>&lt;script&gt;untrusted secret&lt;/script&gt;</title><enclosure url="magnet:?xt=urn:btih:` + f.hash + `"/></item></channel></rss>`
}
func (f *fixture) reopen() {
	f.t.Helper()
	if f.store != nil {
		if e := f.store.Close(); e != nil {
			f.t.Fatal(e)
		}
	}
	var e error
	f.store, e = OpenStore(f.db)
	if e != nil {
		f.t.Fatal(e)
	}
	f.engine = &Engine{Store: f.store, Files: f.files, QB: &QB{HTTPBoundary: NewHTTPBoundary(f.server.URL, ""), Downloads: f.downloads}, Emby: &Emby{NewHTTPBoundary(f.server.URL, "fake-token")}, Library: f.library}
}
func (f *fixture) job() Job {
	f.t.Helper()
	ids, e := f.store.IDs(context.Background())
	if e != nil || len(ids) != 1 {
		f.t.Fatal(ids, e)
	}
	j, e := f.store.Get(context.Background(), ids[0])
	if e != nil {
		f.t.Fatal(e)
	}
	return j
}
func (f *fixture) tick(at int64) {
	f.t.Helper()
	if e := f.engine.Tick(context.Background(), time.Unix(at, 0)); e != nil {
		f.t.Fatal(e)
	}
}
func (f *fixture) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/api/v2/torrents/add":
		if r.Method != "POST" {
			f.t.Error("add method")
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(8 * 1024 * 1024); err != nil {
				f.t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, _, err := r.FormFile("torrents")
			if err != nil {
				f.t.Error(err)
				return
			}
			defer file.Close()
			b, err := io.ReadAll(file)
			hash, parseErr := TorrentHash(b)
			if err != nil || parseErr != nil || hash != f.hash {
				f.t.Error("invalid torrent upload")
			}
			f.multipartAdds++
		} else {
			r.ParseForm()
			if !strings.Contains(r.Form.Get("urls"), f.hash) {
				f.t.Error("hash missing")
			}
		}
		if r.Form.Get("savepath") != f.downloads {
			f.t.Error("savepath mismatch")
		}
		f.adds++
		fmt.Fprint(w, "Ok.")
	case "/api/v2/torrents/info":
		root := f.downloads
		if f.wrongRoot {
			root = "/wrong-root"
		}
		json.NewEncoder(w).Encode([]any{map[string]any{"hash": f.hash, "progress": 1, "completion_on": f.completed, "save_path": root}})
	case "/api/v2/torrents/files":
		if f.failFiles {
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode([]any{map[string]any{"name": f.name, "progress": 1}})
	case "/api/v2/torrents/stop":
		r.ParseForm()
		if r.Method != "POST" || r.Form.Get("hashes") != f.hash {
			f.t.Error("unsafe stop")
		}
		f.stops++
		w.WriteHeader(200)
	case "/Library/Refresh":
		if r.Method != "POST" || r.Header.Get("X-Emby-Token") != "fake-token" {
			f.t.Error("refresh request")
		}
		f.refreshes++
		if f.failRefresh {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	case "/Items":
		if r.URL.Query().Get("api_key") != "" {
			f.t.Error("token in URL")
		}
		items := []any{}
		if f.discovered {
			items = append(items, map[string]any{"Path": r.URL.Query().Get("Path")})
		}
		json.NewEncoder(w).Encode(map[string]any{"Items": items})
	default:
		f.t.Errorf("unexpected API: %s", r.URL.Path)
		w.WriteHeader(404)
	}
}

func TestVerticalLoopReplayAndIndependentSeeding(t *testing.T) {
	f := newFixture(t, "Show.S02E03.mkv", 7)
	if e := os.WriteFile(filepath.Join(f.downloads, "Show.S02E03.zh-Hans.ass"), []byte("subtitle"), 0600); e != nil {
		t.Fatal(e)
	}
	f.tick(1000)
	j := f.job()
	if j.Import != "imported" || j.Refresh != "requested" || j.Discovery != "pending" || j.Seed != "seeding" {
		t.Fatalf("unexpected states: %+v", j)
	}
	if len(j.Plan.Files) != 2 {
		t.Fatal("missing sidecar")
	}
	for _, p := range j.Plan.Files {
		a, e := os.Stat(filepath.Join(f.downloads, p.Source))
		if e != nil {
			t.Fatal(e)
		}
		b, e := os.Stat(filepath.Join(f.library, p.Target))
		if e != nil || !os.SameFile(a, b) {
			t.Fatal("not hardlink", e)
		}
	}
	before, _ := f.store.Events(context.Background(), 0)
	f.reopen()
	if _, e := f.engine.IngestRSS(context.Background(), "fixture", strings.NewReader(f.rss()), 7); e != nil {
		t.Fatal(e)
	}
	f.tick(1001)
	after, _ := f.store.Events(context.Background(), 0)
	if len(before) != len(after) {
		t.Fatal("duplicate events")
	}
	f.mu.Lock()
	f.discovered = true
	f.completed = 9999
	f.mu.Unlock()
	f.tick(1000 + 7*86400 - 1)
	if f.job().Seed != "seeding" {
		t.Fatal("stopped early")
	}
	f.tick(1000 + 7*86400)
	j = f.job()
	if j.Seed != "stopped" || j.Discovery != "confirmed" || j.CompletedAt != 1000 {
		t.Fatalf("unexpected completion: %+v", j)
	}
	f.tick(1000 + 8*86400)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.adds != 1 || f.stops != 1 || f.refreshes != 1 {
		t.Fatal(f.adds, f.stops, f.refreshes)
	}
}

func TestUnknownSeasonStopsImmediatelyAndConfirmation(t *testing.T) {
	f := newFixture(t, "[group] Show - 01.mkv", 0)
	f.tick(1000)
	j := f.job()
	if j.Import != "needs_confirmation" || j.Seed != "stopped" || j.Plan != nil {
		t.Fatalf("guessed season or failed stop: %+v", j)
	}
	ctx := context.Background()
	p, e := f.engine.Propose(ctx, j.ID, f.name, "Show/Season 03/Show - S03E01.mkv")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.engine.Confirm(ctx, j.ID, p.Version-1, PlanDigest(p)); e == nil {
		t.Fatal("stale plan accepted")
	}
	if e = f.engine.Confirm(ctx, j.ID, p.Version, "wrong"); e == nil {
		t.Fatal("wrong digest accepted")
	}
	if e = f.engine.Confirm(ctx, j.ID, p.Version, PlanDigest(p)); e != nil {
		t.Fatal(e)
	}
	f.tick(1001)
	if f.job().Import != "imported" {
		t.Fatal("not imported")
	}
}

func TestRefreshRetryAndCrashAfterLink(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 0)
	ctx := context.Background()
	j := f.job()
	// 模拟完成已落库、硬链接已成功，但 imported 事务前进程崩溃。
	p, e := f.files.Preview(f.name, 2)
	if e != nil {
		t.Fatal(e)
	}
	p.Approved = true
	j.Plan = p
	j.Download = "completed"
	j.CompletedAt = 1000
	j.StopAt = 1000
	j.Seed = "seeding"
	j.Import = "planned"
	j.Sources = []string{f.name}
	if e = f.store.Save(ctx, &j, "plan_ready"); e != nil {
		t.Fatal(e)
	}
	if e = f.files.Link(p.Files[0]); e != nil {
		t.Fatal(e)
	}
	f.reopen()
	f.mu.Lock()
	f.failRefresh = true
	f.mu.Unlock()
	if e = f.engine.Tick(ctx, time.Unix(1000, 0)); e == nil {
		t.Fatal("expected refresh failure")
	}
	j = f.job()
	if j.Import != "imported" || j.Refresh != "pending" || j.Seed != "stopped" {
		t.Fatalf("bad recovery: %+v", j)
	}
	f.mu.Lock()
	f.failRefresh = false
	f.mu.Unlock()
	f.reopen()
	f.tick(1001)
	if f.job().Refresh != "requested" {
		t.Fatal("refresh not retried")
	}
}

func TestFilesFailureDoesNotPreventStop(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 0)
	f.mu.Lock()
	f.failFiles = true
	f.mu.Unlock()
	if e := f.engine.Tick(context.Background(), time.Unix(1000, 0)); e == nil {
		t.Fatal("expected files failure")
	}
	if f.job().Seed != "stopped" {
		t.Fatal("files failure blocked stop")
	}
}

func TestDownloadPathMismatchStillStops(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 0)
	f.mu.Lock()
	f.wrongRoot = true
	f.mu.Unlock()
	if err := f.engine.Tick(context.Background(), time.Unix(1000, 0)); err == nil {
		t.Fatal("expected path mismatch")
	}
	j := f.job()
	if j.Seed != "stopped" || j.Import == "imported" {
		t.Fatal("path mismatch blocked stop or imported")
	}
}
