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

func TestBatchOrganizerBrowserLoop(t *testing.T) {
	if os.Getenv("NAS_BROWSER_TEST") != "1" {
		t.Skip("run make verify-ui")
	}
	f, _ := batchFixture(t)
	_, web := managementFixture(t, f)
	ctx := context.Background()
	// 不透明旧单视频记录来自真实确认，UI必须保持季集未声明。
	opaque := newFixture(t, "unknown.mkv", -1)
	opaqueID := addRelease(t, opaque, "fixture", "one", "")
	opaque.tick(1000)
	opaquePlan, err := opaque.engine.Propose(ctx, opaqueID, opaque.name, "Chosen.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if err = opaque.engine.Confirm(ctx, opaqueID, opaquePlan.Version, PlanDigest(opaquePlan)); err != nil {
		t.Fatal(err)
	}
	_, opaqueWeb := managementFixture(t, opaque)
	conflict := filepath.Join(f.library, "Human library", "Season 04", "Human Example - S04E01.mkv")
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		switch r.URL.Path {
		case "/conflict":
			err = os.MkdirAll(filepath.Dir(conflict), 0700)
			if err == nil {
				err = os.WriteFile(conflict, []byte("other target"), 0600)
			}
		case "/clear-conflict":
			err = os.Remove(conflict)
		case "/assert-preview":
			if f.job().Plan != nil || len(comparisons(t, f)) != 0 {
				t.Error("preview persisted intent")
			}
		case "/partial":
			f.engine.Files = &faultFiles{FileOperations: f.files, at: 2, permission: true}
			if f.engine.Tick(ctx, time.Unix(1001, 0)) == nil || comparisons(t, f)[0].Status != "partial" {
				t.Error("missing partial execution")
			}
		case "/recover":
			f.engine.Files = f.files
			err = f.engine.Tick(ctx, time.Unix(1002, 0))
		case "/discover-one":
			f.engine.Media = &batchPartialMedia{Media: f.engine.Media, firstTarget: f.job().Plan.Files[0].Target}
			err = f.engine.Tick(ctx, time.Unix(1003, 0))
		case "/discover":
			f.engine.Media.(*batchPartialMedia).all = true
			err = f.engine.Tick(ctx, time.Unix(1004, 0))
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
	command := exec.Command("python3", "../../tests/batch_browser.py")
	command.Env = append(os.Environ(), "NAS_WEB_URL="+web.URL, "NAS_FIXTURE_URL="+control.URL, "NAS_OPAQUE_URL="+opaqueWeb.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	j := f.job()
	if j.Import != "imported" || j.Discovery != "confirmed" || len(comparisons(t, f)) != 1 || comparisons(t, f)[0].Status != "complete" {
		t.Fatal("browser false success", j)
	}
	for _, p := range j.Plan.Files {
		src, err := os.Stat(filepath.Join(f.downloads, p.Source))
		if err != nil {
			t.Fatal(err)
		}
		dst, err := os.Stat(filepath.Join(f.library, p.Target))
		if err != nil || !os.SameFile(src, dst) {
			t.Fatal("not hardlink", err)
		}
	}
	if f.adds != 1 {
		t.Fatal("duplicate download", f.adds)
	}
}
