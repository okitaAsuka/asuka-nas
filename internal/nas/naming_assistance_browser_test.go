package nas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 独立入口：真实Chromium、生产管理/服务handler及SQLite；仅模型结果、qB和Emby为fake。
func TestNamingAssistanceBrowserLoop(t *testing.T) {
	if os.Getenv("NAS_BROWSER_TEST") != "1" {
		t.Skip("NAS_BROWSER_TEST=1 go test -race ./internal/nas -run '^TestNamingAssistanceBrowserLoop$' -v")
	}
	ctx := context.Background()
	fixtures := map[string]*fixture{}
	urls := map[string]string{}
	services := map[string]*httptest.Server{}
	for _, name := range []string{"primary", "unresolved", "manual_unresolved", "rejected", "offline", "automatic", "stale", "seed"} {
		var f *fixture
		if name == "primary" || name == "offline" {
			f, _ = batchFixture(t)
		} else {
			f = namingFixture(t)
		}
		fixtures[name] = f
		if name == "automatic" {
			if err := namingDaemon(f, true).TickJobs(ctx, time.Unix(1001, 0)); err != nil {
				t.Fatal(err)
			}
		}
		m, web := managementFixture(t, f)
		if name == "automatic" {
			m.Daemon.Config.Learning.Enabled = true
			m.Daemon.Config.Learning.AutomaticNaming = true
		}
		urls[name] = web.URL
		service := httptest.NewUnstartedServer(nil)
		service.Config.Handler = LearningHandler(f.store, learningFixtureToken, service.Listener.Addr().String(), f.engine)
		if name == "offline" {
			service.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { apiError(w, 503, "fixture_offline") })
		}
		service.Start()
		t.Cleanup(service.Close)
		services[name] = service
	}
	var oldDigest string
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 2 || fixtures[parts[0]] == nil {
			w.WriteHeader(404)
			return
		}
		f, action := fixtures[parts[0]], parts[1]
		fail := func(err error) { t.Error(err); w.WriteHeader(500) }
		reviews, err := f.engine.NamingReviews(ctx, f.job().ID)
		if err != nil {
			fail(err)
			return
		}
		var v NamingReview
		if len(reviews) > 0 {
			v = reviews[len(reviews)-1]
		}
		switch action {
		case "suggest", "unresolved":
			// fake消费者从生产鉴权队列获取快照，再通过生产结果端点提交有限DTO。
			req, _ := http.NewRequest("GET", services[parts[0]].URL+"/v1/naming/requests?after=0", nil)
			req.Header.Set("Authorization", "Bearer "+learningFixtureToken)
			res, e := services[parts[0]].Client().Do(req)
			if e != nil {
				fail(e)
				return
			}
			var page struct {
				Items []struct {
					Snapshot NamingSnapshot `json:"snapshot"`
				} `json:"items"`
			}
			e = json.NewDecoder(res.Body).Decode(&page)
			res.Body.Close()
			if e != nil || res.StatusCode != 200 || len(page.Items) == 0 {
				fail(fmt.Errorf("queue %v status %d", e, res.StatusCode))
				return
			}
			snapshot := page.Items[len(page.Items)-1].Snapshot
			result := NamingResult{Protocol: 1, ID: snapshot.ID, Digest: snapshot.Digest, Status: "unresolved", Explanation: "缺少明确目录、显示名与季号", Uncertainties: []string{"请人工补充媒体目录、显示名、季号"}}
			targets := map[string]PlannedFile{}
			episodes := map[string]int{}
			if action == "suggest" {
				result.Status = "suggestion"
				result.Directory = snapshot.Hints.Directory
				result.DisplayName = snapshot.Hints.DisplayName
				result.Explanation = `<img src=x onerror="window.PWNED=1"> 人工季号与明确文件编号`
				result.Uncertainties = []string{"未来集数未验证，不启用规则"}
				b := BatchOrganization{Directory: result.Directory, DisplayName: result.DisplayName, Season: snapshot.Hints.Season, Mode: "manual"}
				for _, file := range snapshot.Files {
					if file.Kind == "video" {
						source := ""
						for _, s := range f.job().Sources {
							if digestString(s) == file.ID {
								source = s
							}
						}
						ep := file.Episode
						if parts[0] == "primary" {
							ep -= 66
						}
						b.Rows = append(b.Rows, BatchRow{Source: source, Episode: ep})
					}
				}
				preview, e := f.engine.BatchPreview(ctx, f.job().ID, f.job().Version, b)
				if e != nil {
					fail(e)
					return
				}
				for _, p := range preview.Plan.Files {
					targets[digestString(p.Source)] = p
					for _, row := range b.Rows {
						if strings.HasPrefix(p.Source, strings.TrimSuffix(row.Source, filepath.Ext(row.Source))) {
							episodes[digestString(p.Source)] = row.Episode
						}
					}
				}
			}
			for _, file := range snapshot.Files {
				row := NamingSuggestionRow{FileID: file.ID, Basis: "完整清单 + 人工事实", Uncertainties: []string{}}
				if p, ok := targets[file.ID]; ok {
					row.Target = p.Target
					row.Season = snapshot.Hints.Season
					row.Episode = episodes[file.ID]
				}
				result.Files = append(result.Files, row)
			}
			body, _ := json.Marshal(result)
			req, _ = http.NewRequest("POST", services[parts[0]].URL+"/v1/naming/results", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+learningFixtureToken)
			req.Header.Set("Content-Type", "application/json")
			res, e = services[parts[0]].Client().Do(req)
			if e != nil {
				fail(e)
				return
			}
			output, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != 200 {
				fail(fmt.Errorf("result status %d %s", res.StatusCode, output))
				return
			}
		case "stop-seed":
			if e := f.engine.Tick(ctx, time.Unix(f.job().StopAt, 0)); e != nil {
				fail(e)
				return
			}
			current, e := f.engine.NamingReviews(ctx, f.job().ID)
			if e != nil || len(current) != 1 || current[0].State != "suggested" || f.job().Seed != "stopped" {
				fail(fmt.Errorf("seeding invalidated suggestion %v", e))
				return
			}
		case "stale":
			if e := f.store.AttachResourceTitle(ctx, "fixture", "private-guid", "Example additional [01]"); e != nil {
				fail(e)
				return
			}
		case "assert-no-requeue":
			if e := namingDaemon(f, true).TickJobs(ctx, time.Unix(1003, 0)); e != nil {
				fail(e)
				return
			}
			if requestCount(t, f) != 1 {
				fail(fmt.Errorf("fallback requeued naming"))
				return
			}
		case "assert-offline":
			res, e := services[parts[0]].Client().Get(services[parts[0]].URL + "/v1/naming/requests?after=0")
			if e != nil {
				fail(e)
				return
			}
			res.Body.Close()
			if res.StatusCode != 503 || v.State != "requested" {
				fail(fmt.Errorf("offline state"))
				return
			}
		case "assert-preview":
			entries, e := os.ReadDir(f.library)
			if e != nil || len(entries) != 0 || f.job().Plan != nil || len(comparisons(t, f)) != 0 {
				fail(fmt.Errorf("preview wrote media or intent"))
				return
			}
		case "save-digest":
			if v.Preview == nil {
				fail(fmt.Errorf("missing preview"))
				return
			}
			oldDigest = v.Preview.Digest
		case "assert-old-digest":
			if oldDigest == "" || f.engine.ConfirmNaming(ctx, v.ID, v.Version, oldDigest) == nil {
				fail(fmt.Errorf("old digest accepted"))
				return
			}
		case "assert-rejected":
			if len(reviews) != 1 || v.State != "rejected" || f.job().Plan != nil {
				fail(fmt.Errorf("rejection restarted or approved"))
				return
			}
		case "execute":
			if e := f.engine.Tick(ctx, time.Unix(1001, 0)); e != nil {
				fail(e)
				return
			}
			if f.job().Import != "imported" || f.job().Discovery == "confirmed" {
				fail(fmt.Errorf("wrong linked state"))
				return
			}
		case "discover":
			f.mu.Lock()
			f.discovered = true
			f.mu.Unlock()
			if e := f.engine.Tick(ctx, time.Unix(1002, 0)); e != nil {
				fail(e)
				return
			}
		default:
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer control.Close()
	encoded, _ := json.Marshal(urls)
	command := exec.Command("python3", "../../tests/naming_assistance_browser.py")
	command.Env = append(os.Environ(), "NAS_NAMING_URLS="+string(encoded), "NAS_FIXTURE_URL="+control.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	rejected := fixtures["rejected"]
	rejected.reopen()
	reopened, reopenErr := rejected.engine.NamingReviews(ctx, rejected.job().ID)
	if reopenErr != nil || len(reopened) != 1 || reopened[0].State != "rejected" {
		t.Fatal("rejection lost after reopen", reopenErr)
	}
	for _, name := range []string{"primary", "unresolved", "manual_unresolved", "offline", "rejected", "automatic", "stale", "seed"} {
		f := fixtures[name]
		j := f.job()
		if j.Import != "imported" || j.Discovery != "confirmed" || len(comparisons(t, f)) != 1 {
			t.Fatal("false browser completion", name, j)
		}
		if comparisons(t, f)[0].Intent.Origin != "model_assisted_human" {
			t.Fatal("model origin lost", name)
		}
		samples, sampleErr := f.store.LearningSamples(ctx, 0)
		if sampleErr != nil || len(samples.Items) != 0 {
			t.Fatal("assistance history washed into learning", name, sampleErr)
		}
		if name == "seed" && j.Seed != "stopped" {
			t.Fatal("seed stop lost", j)
		}
		if name == "offline" && comparisons(t, f)[0].Intent.Assistance.ResultDigest != "" {
			t.Fatal("invented model result")
		}
		for _, p := range j.Plan.Files {
			src, e := os.Stat(filepath.Join(f.downloads, p.Source))
			if e != nil {
				t.Fatal(e)
			}
			dst, e := os.Stat(filepath.Join(f.library, p.Target))
			if e != nil || !os.SameFile(src, dst) {
				t.Fatal("not hardlink", name, e)
			}
		}
	}
}
