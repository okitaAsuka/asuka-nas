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
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		switch r.URL.Path {
		case "/tick":
			err = f.engine.Tick(ctx, time.Unix(1001, 0))
		case "/discover":
			f.mu.Lock()
			f.discovered = true
			f.mu.Unlock()
			err = f.engine.Tick(ctx, time.Unix(1002, 0))
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
	command.Env = append(os.Environ(), "NAS_WEB_URL="+web.URL, "NAS_FIXTURE_URL="+control.URL)
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
