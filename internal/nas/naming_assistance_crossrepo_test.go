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

// TestNamingGatewaySource 暴露真实生产handler给另一仓worker，模型唯一stub；媒体/qB/Emby全部fake。
func TestNamingGatewaySource(t *testing.T) {
	dir := os.Getenv("NAS_NAMING_SMOKE_DIR")
	if dir == "" {
		t.Skip("explicit cross-repo fake smoke only")
	}
	type wire struct {
		Name          string `json:"name"`
		LearningURL   string `json:"learning_url"`
		ManagementURL string `json:"management_url"`
		JobID         string `json:"job_id"`
		JobVersion    int    `json:"job_version"`
	}
	type scenario struct {
		wire       wire
		f          *fixture
		m          *Management
		posts      atomic.Int32
		handled    bool
		restarted  bool
		progressed bool
	}
	scenarios := []*scenario{}
	ready := []wire{}
	for _, name := range []string{"primary", "offline", "rejected", "stale", "conflict", "unresolved", "budget", "invalid", "seed_progress", "automatic", "manual_stale", "manual_offline"} {
		f := namingFixture(t)
		if name == "primary" {
			f.mu.Lock()
			f.manifest = []string{f.name, "Example [01].zh-Hans.ass", "Example [02].mp4", "notes.txt"}
			f.mu.Unlock()
			for _, file := range f.manifest {
				if err := os.WriteFile(filepath.Join(f.downloads, file), []byte(file), 0600); err != nil {
					t.Fatal(err)
				}
			}
			j := f.job()
			j.Sources = f.manifest
			if err := f.store.Save(context.Background(), &j, "fixture_manifest"); err != nil {
				t.Fatal(err)
			}
		}
		if name == "automatic" {
			if err := namingDaemon(f, true).TickJobs(context.Background(), time.Unix(1001, 0)); err != nil {
				t.Fatal(err)
			}
			if requestCount(t, f) != 1 {
				t.Fatal("automatic did not enqueue")
			}
		}
		m, management := managementFixture(t, f)
		s := &scenario{f: f, m: m}
		server := httptest.NewUnstartedServer(nil)
		server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if name == "offline" || name == "manual_offline" {
				if _, err := os.Stat(filepath.Join(dir, name+"-connect")); err != nil {
					apiError(w, 503, "fixture_offline")
					return
				}
			}
			handler := LearningHandler(f.store, learningFixtureToken, server.Listener.Addr().String(), f.engine)
			if name == "primary" && r.Method == "POST" && r.URL.Path == "/v1/naming/results" {
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, r)
				if rec.Code == 200 && s.posts.Add(1) == 1 {
					apiError(w, 503, "fixture_ack_lost")
					return
				}
				for k, v := range rec.Header() {
					w.Header()[k] = v
				}
				w.WriteHeader(rec.Code)
				w.Write(rec.Body.Bytes())
				return
			}
			handler.ServeHTTP(w, r)
		})
		server.Start()
		t.Cleanup(server.Close)
		s.wire = wire{name, server.URL, management.URL, f.job().ID, f.job().Version}
		scenarios = append(scenarios, s)
		ready = append(ready, s.wire)
	}
	write := func(name string, v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("ready.json", map[string]any{"cases": ready})
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, s := range scenarios {
			name := s.wire.Name

			if name == "manual_stale" && !s.progressed {
				if _, err := os.Stat(filepath.Join(dir, "manual_stale-context-change")); err == nil {
					if err = s.f.store.AttachResourceTitle(context.Background(), "fixture", "private-guid", "Example changed [01]"); err != nil {
						t.Fatal(err)
					}
					s.progressed = true
					write("manual_stale-context-changed", true)
				}
			}
			if name == "seed_progress" && !s.progressed {
				if _, err := os.Stat(filepath.Join(dir, "seed_progress-model-started")); err == nil {
					original := s.f.job().Version
					s.f.tick(s.f.job().StopAt)
					if s.f.job().Version == original || s.f.job().Seed != "stopped" || requestCount(t, s.f) != 1 {
						t.Fatal("seed fixture did not advance")
					}
					s.progressed = true
					write("seed_progress-stopped", true)
				}
			}
			if name == "automatic" && !s.progressed {
				if _, err := os.Stat(filepath.Join(dir, "automatic-recheck")); err == nil {
					s.f.reopen()
					s.m.Daemon.Engine = s.f.engine
					d := namingDaemon(s.f, true)
					for i := 0; i < 3; i++ {
						if err := d.TickJobs(context.Background(), time.Unix(s.f.job().StopAt+int64(i), 0)); err != nil {
							t.Fatal(err)
						}
					}
					if requestCount(t, s.f) != 1 || s.f.job().Seed != "stopped" {
						t.Fatal("automatic unresolved/restart/seed duplicated")
					}
					s.progressed = true
					write("automatic-rechecked", true)
				}
			}
			if name == "primary" && !s.restarted {
				if _, err := os.Stat(filepath.Join(dir, "primary-restart")); err == nil {
					s.f.reopen()
					s.m.Daemon.Engine = s.f.engine
					s.restarted = true
					write("primary-reopened", true)
				}
			}
			if name == "stale" && !s.handled {
				if _, err := os.Stat(filepath.Join(dir, "stale-model-returned")); err == nil {
					if err = s.f.store.AttachResourceTitle(context.Background(), "fixture", "private-guid", "Example changed [01]"); err != nil {
						t.Fatal(err)
					}
					s.handled = true
					write("stale-evidence-changed", true)
				}
			}
			if name == "conflict" && !s.handled {
				if _, err := os.Stat(filepath.Join(dir, "conflict-before-confirm")); err == nil {
					target := filepath.Join(s.f.library, "Example/Season 02/Example - S02E01.mkv")
					if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(target, []byte("other owner"), 0600); err != nil {
						t.Fatal(err)
					}
					s.handled = true
					write("conflict-created", true)
				}
			}
			if (name == "primary" || name == "offline" || name == "seed_progress" || name == "automatic" || name == "rejected" || name == "manual_stale" || name == "manual_offline") && !s.handled {
				if _, err := os.Stat(filepath.Join(dir, name+"-approved")); err == nil {
					f := s.f
					f.mu.Lock()
					f.discovered = true
					f.mu.Unlock()
					f.tick(1010)
					j := f.job()
					if j.Import != "imported" || j.Discovery != "confirmed" || j.Plan.Assistance == nil {
						t.Fatal("fake completion missing", name)
					}
					for _, file := range j.Plan.Files {
						src, err := os.Stat(filepath.Join(f.downloads, file.Source))
						if err != nil {
							t.Fatal(err)
						}
						dst, err := os.Stat(filepath.Join(f.library, file.Target))
						if err != nil || !os.SameFile(src, dst) {
							t.Fatal("not hardlink", name)
						}
					}
					if name == "primary" && (len(j.Plan.Manifest) != 4 || len(j.Plan.Files) != 3) {
						t.Fatal("partial collection imported")
					}
					records := comparisons(t, f)
					if len(records) != 1 || records[0].Intent.Origin != "model_assisted_human" || records[0].Status != "complete" {
						t.Fatal("human attribution forged")
					}
					page, err := f.store.LearningSamples(context.Background(), 0)
					if err != nil || len(page.Items) != 0 {
						t.Fatal("model self-learning")
					}
					s.handled = true
					write(name+"-complete", true)
				}
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "done")); err == nil {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("cross-repo naming timeout")
		case <-ticker.C:
		}
	}
	for _, s := range scenarios {
		views, err := s.f.engine.NamingReviews(context.Background(), s.wire.JobID)
		if err != nil || len(views) < 1 {
			t.Fatal(err)
		}
		if (s.wire.Name == "seed_progress" || s.wire.Name == "automatic") && (!s.progressed || requestCount(t, s.f) != 1 || s.f.job().Seed != "stopped") {
			t.Fatal("semantic/automatic quota regression")
		}
		if s.wire.Name == "rejected" || s.wire.Name == "manual_stale" || s.wire.Name == "manual_offline" {
			if requestCount(t, s.f) != 1 {
				t.Fatal("manual fallback re-requested model")
			}
			original, _, err := s.f.store.namingReview(context.Background(), views[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			expected := "suggested"
			if s.wire.Name == "rejected" {
				expected = "rejected"
			}
			if s.wire.Name == "manual_offline" {
				expected = "requested"
			}
			if original.State != expected || s.f.job().Plan.NamingReview {
				t.Fatal("manual fallback rewrote original model audit")
			}
		}
		if s.wire.Name == "primary" && (!s.restarted || s.posts.Load() != 2) {
			t.Fatal("lost ACK not durably replayed")
		}
		if s.wire.Name != "primary" && s.wire.Name != "offline" && s.wire.Name != "seed_progress" && s.wire.Name != "automatic" && s.wire.Name != "rejected" && s.wire.Name != "manual_stale" && s.wire.Name != "manual_offline" && s.f.job().Import == "imported" {
			t.Fatal("unsafe scenario executed")
		}
		events, err := s.f.store.Events(context.Background(), 0)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(events)
		for _, secret := range []string{"Example", "magnet:", "private-guid", "fixture-only-token", s.f.downloads} {
			if strings.Contains(string(b), secret) {
				t.Fatal("event leak")
			}
		}
		var stateCount int
		if err = s.f.store.db.QueryRow(`SELECT count(*) FROM naming_assistance_audit`).Scan(&stateCount); err != nil || stateCount < 1 {
			t.Fatal("missing audit")
		}
	}
	fmt.Println("PASS real NAS SQLite handlers + full media manifest -> agent budget/model stub -> durable result/lost ACK/both restarts -> human current preview -> actual hardlinks/fakeEmby; offline/reject/stale/conflict/unresolved/budget/invalid/seed_progress/automatic-once-human-facts + rejected/stale/offline manual batch without re-inference")
}
