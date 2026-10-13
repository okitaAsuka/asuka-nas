package nas

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const learningFixtureToken = "fixture-only-token-01234567890123456789"

// TestRuleProposalGatewaySource 仅显式跨仓fake入口，真实管理/学习handler+SQLite，媒体只在TempDir。
func TestRuleProposalGatewaySource(t *testing.T) {
	dir := os.Getenv("NAS_LEARNING_SMOKE_DIR")
	if dir == "" {
		t.Skip("explicit cross-repo fake smoke only")
	}
	type wireCase struct {
		Name          string `json:"name"`
		LearningURL   string `json:"learning_url"`
		ManagementURL string `json:"management_url"`
	}
	type scenario struct {
		wire     wireCase
		f        *fixture
		ref      SampleRef
		original string
		posts    atomic.Int32
		handled  bool
	}
	cases := []*scenario{}
	ready := []wireCase{}
	for _, name := range []string{"primary", "rejected", "stale"} {
		f, sample, _ := learningFixture(t)
		item := &scenario{f: f, ref: sample.Ref, original: digestStringJSON(comparisons(t, f)[0].Intent)}
		learning := httptest.NewUnstartedServer(nil)
		handler := LearningHandler(f.store, learningFixtureToken, learning.Listener.Addr().String())
		learning.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 第一份成功提交已落库后丢ACK，重启只应重放同一持久候选。
			if r.Method == "POST" && r.URL.Path == "/v1/learning/proposals" {
				result := httptest.NewRecorder()
				handler.ServeHTTP(result, r)
				if result.Code == 200 && item.posts.Add(1) == 1 {
					apiError(w, 503, "fixture_ack_lost")
					return
				}
				for k, values := range result.Header() {
					w.Header()[k] = values
				}
				w.WriteHeader(result.Code)
				w.Write(result.Body.Bytes())
				return
			}
			handler.ServeHTTP(w, r)
		})
		learning.Start()
		t.Cleanup(learning.Close)
		_, management := managementFixture(t, f)
		item.wire = wireCase{name, learning.URL, management.URL}
		cases = append(cases, item)
		ready = append(ready, item.wire)
	}
	b, _ := json.Marshal(map[string]any{"cases": ready})
	if e := os.WriteFile(filepath.Join(dir, "ready.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		for _, item := range cases {
			name := item.wire.Name
			if name == "stale" && !item.handled {
				if _, e := os.Stat(filepath.Join(dir, "stale-model-returned")); e == nil {
					j := item.f.job()
					if e = item.f.store.AttachResourceTitle(context.Background(), "fixture", "private-guid", "Example changed [01]"); e != nil {
						t.Fatal(e)
					}
					now := item.f.job()
					if now.ContextVersion == j.ContextVersion {
						t.Fatal("stale signal failed")
					}
					item.handled = true
					if e = os.WriteFile(filepath.Join(dir, "stale-evidence-changed"), []byte("ok"), 0600); e != nil {
						t.Fatal(e)
					}
				}
			}
			if name == "primary" && !item.handled {
				if _, e := os.Stat(filepath.Join(dir, "primary-approved")); e == nil {
					f := item.f
					ctx := context.Background()
					old := f.job()
					epoch, e := f.store.NamingEpoch(ctx)
					if e != nil || epoch != 1 {
						t.Fatal("approval missing", epoch, e)
					}
					if digestStringJSON(comparisons(t, f)[0].Intent) != item.original {
						t.Fatal("scope rewrote original intent")
					}
					f.mu.Lock()
					f.hash = "1123456789abcdef0123456789abcdef01234567"
					f.name = "Example [02].mkv"
					f.manifest = nil
					f.mu.Unlock()
					if e = os.WriteFile(filepath.Join(f.downloads, f.name), []byte("episode two fixture"), 0600); e != nil {
						t.Fatal(e)
					}
					id, e := f.store.IngestRelease(ctx, "fixture", "next-resource", f.hash, "magnet:?xt=urn:btih:"+f.hash, nil, 7, "Example [02]")
					if e != nil {
						t.Fatal(e)
					}
					f.tick(1010)
					j, e := f.store.Get(ctx, id)
					if e != nil {
						t.Fatal(e)
					}
					target := "Example/Season 02/Example - S02E02.mkv"
					if j.Import != "imported" || j.Discovery != "confirmed" || j.BlockReason != "" || j.Plan == nil || j.Plan.Batch != nil || len(j.Plan.RuleRefs) != 1 || j.Plan.Files[0].Target != target {
						t.Fatal("E02 did not automatically use approved rule", j.Import, j.Discovery, j.BlockReason, e)
					}
					src, e := os.Stat(filepath.Join(f.downloads, f.name))
					if e != nil {
						t.Fatal(e)
					}
					dst, e := os.Stat(filepath.Join(f.library, target))
					if e != nil || !os.SameFile(src, dst) {
						t.Fatal("not hardlink", e)
					}
					intent, _, _ := f.store.ComparisonPage(ctx, old.ID, 0)
					if len(intent) != 1 || digestStringJSON(intent[0].Intent) != item.original {
						t.Fatal("old sample changed after E02")
					}
					var manualCount int
					if e = f.store.db.QueryRow(`SELECT count(*) FROM comparison_records`).Scan(&manualCount); e != nil || manualCount != 1 {
						t.Fatal("auto task forged human sample", manualCount, e)
					}
					f.mu.Lock()
					adds := f.adds
					f.mu.Unlock()
					if adds != 2 {
						t.Fatal("unexpected downloads", adds)
					}
					item.handled = true
					if e = os.WriteFile(filepath.Join(dir, "primary-auto-ready"), []byte("ok"), 0600); e != nil {
						t.Fatal(e)
					}
				}
			}
		}
		if _, e := os.Stat(filepath.Join(dir, "done")); e == nil {
			for _, item := range cases {
				name := item.wire.Name
				epoch, e := item.f.store.NamingEpoch(context.Background())
				if e != nil {
					t.Fatal(e)
				}
				if name == "primary" {
					if !item.handled || item.posts.Load() != 2 {
						t.Fatal("ACK replay failure")
					}
				} else if epoch != 0 {
					t.Fatal("rejected/stale enabled", name, epoch)
				}
				rows, _, e := item.f.store.ProposalList(context.Background(), 0)
				if e != nil {
					t.Fatal(e)
				}
				if name == "rejected" && (len(rows) != 1 || rows[0].State != "rejected") {
					t.Fatal("rejection missing", rows)
				}
				if name == "stale" && len(rows) != 0 {
					t.Fatal("stale evidence accepted")
				}
				events, e := item.f.store.Events(context.Background(), 0)
				if e != nil {
					t.Fatal(e)
				}
				raw, _ := json.Marshal(events)
				for _, secret := range []string{"Example", learningFixtureToken, "private-guid", "magnet:", item.f.downloads} {
					if strings.Contains(string(raw), secret) {
						t.Fatal("private event escaped")
					}
				}
			}
			fmt.Println("PASS cross-repo E01 -> persisted agent candidate/ACK replay -> explicit human E01-12 -> automatic E02 hardlink/discovery; reject/stale preserved")
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("agent cross-repo fixture did not finish")
		case <-tick.C:
		}
	}
}
