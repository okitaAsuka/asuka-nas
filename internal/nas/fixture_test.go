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
	requests                     int
	manifest                     []string
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
	// 先登记清理，后续任一步 Fatal 也不会遗留已创建的 HTTP/fd/SQLite。
	t.Cleanup(func() {
		if f.server != nil {
			f.server.Close()
		}
		if f.files != nil {
			f.files.Close()
		}
		if f.store != nil {
			f.store.Close()
		}
	})
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
	f.requests++
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
		names := f.manifest
		if names == nil {
			names = []string{f.name}
		}
		var list []any
		for _, name := range names {
			list = append(list, map[string]any{"name": name, "progress": 1})
		}
		json.NewEncoder(w).Encode(list)
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
