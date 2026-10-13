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

func TestLibraryCandidateBrowserLoop(t *testing.T) {
	if os.Getenv("NAS_BROWSER_TEST") != "1" {
		t.Skip("run make verify-ui")
	}
	ctx := context.Background()
	f := newFixture(t, "Example [02].mkv", -1)
	seedLibrary(t, f.library)
	m, web := managementFixture(t, f)
	m.Daemon.Config.Library = f.library
	if _, e := f.store.IngestRelease(ctx, "fixture", "guid", f.hash, "magnet:?xt=urn:btih:"+f.hash, nil, 0, "Example Arc [02]"); e != nil {
		t.Fatal(e)
	}
	if e := f.engine.Tick(ctx, time.Unix(1001, 0)); e != nil {
		t.Fatal(e)
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		switch r.URL.Path {
		case "/change-library":
			libraryFile(t, f.library, "Example/changed.txt", "change after preview")
		case "/tick":
			err = f.engine.Tick(ctx, time.Unix(1002, 0))
		case "/discover":
			f.mu.Lock()
			f.discovered = true
			f.mu.Unlock()
			err = f.engine.Tick(ctx, time.Unix(1003, 0))
		case "/next":
			f.mu.Lock()
			f.hash = "1123456789abcdef0123456789abcdef01234567"
			f.name = "Example [03].mkv"
			err = os.WriteFile(filepath.Join(f.downloads, f.name), []byte("next episode"), 0600)
			f.mu.Unlock()
			if err == nil {
				_, err = f.store.IngestRelease(ctx, "fixture", "guid-next", f.hash, "magnet:?xt=urn:btih:"+f.hash, nil, 0, "Example Arc [03]")
			}
			if err == nil {
				err = f.engine.Tick(ctx, time.Unix(1004, 0))
			}
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
	cmd := exec.Command("python3", "../../tests/library_browser.py")
	cmd.Env = append(os.Environ(), "NAS_WEB_URL="+web.URL, "NAS_FIXTURE_URL="+control.URL)
	output, e := cmd.CombinedOutput()
	t.Log(string(output))
	if e != nil {
		t.Fatal(e)
	}
	ids, e := f.store.IDs(ctx)
	if e != nil || len(ids) != 2 || f.adds != 2 {
		t.Fatal(ids, e, f.adds)
	}
	for _, id := range ids {
		j, e := f.store.Get(ctx, id)
		if e != nil || j.Import != "imported" || j.Discovery != "confirmed" {
			t.Fatal(j, e)
		}
		for _, p := range j.Plan.Files {
			src, e := os.Stat(filepath.Join(f.downloads, p.Source))
			if e != nil {
				t.Fatal(e)
			}
			dst, e := os.Stat(filepath.Join(f.library, p.Target))
			if e != nil || !os.SameFile(src, dst) {
				t.Fatal("hardlink/source retention", e)
			}
		}
	}
	events, e := f.store.Events(ctx, 0)
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]int{}
	for _, v := range events {
		if v.Kind == "discovered" {
			seen[v.JobID]++
		}
	}
	for _, id := range ids {
		if seen[id] != 1 {
			t.Fatal("event duplicated or missing", seen)
		}
	}
}
