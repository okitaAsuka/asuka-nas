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
	"testing"
)

func namingFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, "Example [01].mkv", -1)
	if _, err := f.store.IngestRelease(context.Background(), "fixture", "private-guid", f.hash, "magnet:?xt=urn:btih:"+f.hash, nil, 7, "Example [01]"); err != nil {
		t.Fatal(err)
	}
	f.tick(1000)
	if f.job().Import != "needs_confirmation" {
		t.Fatal("not unknown")
	}
	return f
}
func namingRequest(t *testing.T, f *fixture) NamingReview {
	t.Helper()
	j := f.job()
	v, err := f.engine.RequestNaming(context.Background(), j.ID, j.Version, NamingHints{"Example", "Example", 2}, false)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func namingResult(v NamingReview) NamingResult {
	r := NamingResult{Protocol: 1, ID: v.ID, Digest: v.Snapshot.Digest, Status: "suggestion", Directory: "Example", DisplayName: "Example", Explanation: "人工明确季号，文件已有集号", Uncertainties: []string{}}
	for _, f := range v.Snapshot.Files {
		r.Files = append(r.Files, NamingSuggestionRow{f.ID, 2, 1, "Example/Season 02/Example - S02E01.mkv", "文件明确第一集", []string{}})
	}
	return r
}
func TestNamingAssistanceApprovalAndProvenance(t *testing.T) {
	ctx := context.Background()
	f := namingFixture(t)
	v := namingRequest(t, f)
	r := namingResult(v)
	again := namingRequest(t, f)
	if v.ID != again.ID {
		t.Fatal("duplicate request")
	}
	submitted, err := f.engine.SubmitNaming(ctx, r)
	if err != nil || submitted.Version != 2 {
		t.Fatal(err)
	}
	if err = f.engine.ConfirmNaming(ctx, v.ID, 2, "missing"); err == nil {
		t.Fatal("approved without preview")
	}
	preview, err := f.engine.PreviewNaming(ctx, v.ID, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.engine.ConfirmNaming(ctx, v.ID, 2, preview.Digest); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	f.tick(1001)
	j := f.job()
	if j.Import != "imported" || j.Discovery != "confirmed" || j.Plan.Assistance == nil {
		t.Fatal("not complete")
	}
	src, _ := os.Stat(filepath.Join(f.downloads, f.name))
	dst, err := os.Stat(filepath.Join(f.library, j.Plan.Files[0].Target))
	if err != nil || !os.SameFile(src, dst) {
		t.Fatal("not hardlink")
	}
	records := comparisons(t, f)
	if len(records) != 1 || records[0].Intent.Origin != "model_assisted_human" || records[0].Intent.Assistance.ID != v.ID {
		t.Fatal("forged human record")
	}
	page, err := f.store.LearningSamples(ctx, 0)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("model loop")
	}
	f.reopen()
	if err = f.engine.ConfirmNaming(ctx, v.ID, 2, preview.Digest); err != nil {
		t.Fatal("approval replay", err)
	}
	if _, err = f.engine.SubmitNaming(ctx, r); err != nil {
		t.Fatal("ACK replay", err)
	}
	if len(comparisons(t, f)) != 1 {
		t.Fatal("duplicate human record")
	}
}
func TestNamingAssistanceBoundaries(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"context", "fingerprint", "epoch", "conflict", "rejected", "preview_replaced", "incomplete", "unknown_season", "budget_requests", "rollback", "manual_provenance", "private_title", "private_path", "unsupported", "rules"} {
		t.Run(name, func(t *testing.T) {
			f := namingFixture(t)
			if name == "private_title" || name == "private_path" {
				title := "https://private.invalid/feed?token=secret"
				if name == "private_path" {
					title = "Example /private/library [01]"
				}
				if err := f.store.AttachResourceTitle(ctx, "fixture", "private-guid", title); err != nil {
					t.Fatal(err)
				}
				if _, err := f.engine.RequestNaming(ctx, f.job().ID, f.job().Version, NamingHints{}, false); err == nil {
					t.Fatal("private exported")
				}
				return
			}
			if name == "unsupported" {
				f.mu.Lock()
				f.manifest = []string{"Example OVA.mkv"}
				f.mu.Unlock()
				if err := os.WriteFile(filepath.Join(f.downloads, "Example OVA.mkv"), []byte("special"), 0600); err != nil {
					t.Fatal(err)
				}
				j := f.job()
				j.Sources = f.manifest
				if err := f.store.Save(ctx, &j, "fixture_changed"); err != nil {
					t.Fatal(err)
				}
				if _, err := f.engine.RequestNaming(ctx, j.ID, j.Version, NamingHints{}, false); err == nil {
					t.Fatal("special accepted")
				}
				return
			}
			if name == "rules" {
				r := manualRule()
				r.Subscription = "fixture"
				r.Group = ""
				r.Aliases = []string{"Example"}
				r.Season = 2
				installRule(t, f.store, r)
				if _, err := f.engine.RequestNaming(ctx, f.job().ID, f.job().Version, NamingHints{}, false); err == nil {
					t.Fatal("rules bypassed")
				}
				return
			}
			v := namingRequest(t, f)
			r := namingResult(v)
			switch name {
			case "context":
				if err := f.store.AttachResourceTitle(ctx, "fixture", "private-guid", "Example changed [01]"); err != nil {
					t.Fatal(err)
				}
			case "fingerprint":
				if err := os.WriteFile(filepath.Join(f.downloads, f.name), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "epoch":
				installRule(t, f.store, manualRule())
			case "incomplete":
				r.Files = nil
			case "unknown_season":
				if err := f.engine.RejectNaming(ctx, v.ID, v.Version); err != nil {
					t.Fatal(err)
				}
				v, err := f.engine.RequestNaming(ctx, f.job().ID, f.job().Version, NamingHints{"Example", "Example", 0}, true)
				if err != nil {
					t.Fatal(err)
				}
				r = namingResult(v)
			}
			if name == "context" || name == "fingerprint" || name == "epoch" || name == "incomplete" || name == "unknown_season" {
				if _, err := f.engine.SubmitNaming(ctx, r); err == nil {
					t.Fatal("unsafe accepted")
				}
				return
			}
			accepted, err := f.engine.SubmitNaming(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			changed := r
			changed.Explanation = "different"
			if _, err = f.engine.SubmitNaming(ctx, changed); err == nil || err.Error() != "naming_replay_conflict" {
				t.Fatal("replay conflict")
			}
			if name == "rejected" || name == "budget_requests" {
				if err = f.engine.RejectNaming(ctx, v.ID, accepted.Version); err != nil {
					t.Fatal(err)
				}
				if _, err = f.engine.PreviewNaming(ctx, v.ID, accepted.Version, nil); err == nil {
					t.Fatal("rejected preview")
				}
				if replay, err := f.engine.SubmitNaming(ctx, r); err != nil || replay.State != "rejected" {
					t.Fatal("rejected replay", err)
				}
				if name == "budget_requests" {
					for i := 0; i < 2; i++ {
						next, err := f.engine.RequestNaming(ctx, f.job().ID, f.job().Version, v.Snapshot.Hints, true)
						if err != nil {
							t.Fatal(err)
						}
						if err = f.engine.RejectNaming(ctx, next.ID, next.Version); err != nil {
							t.Fatal(err)
						}
					}
					if _, err = f.engine.RequestNaming(ctx, f.job().ID, f.job().Version, v.Snapshot.Hints, true); err == nil {
						t.Fatal("request budget")
					}
				}
				return
			}
			preview, err := f.engine.PreviewNaming(ctx, v.ID, accepted.Version, nil)
			if err != nil {
				t.Fatal(err)
			}
			if name == "preview_replaced" {
				b := *preview.Plan.Batch
				b.Rows[0].Episode = 2
				newPreview, err := f.engine.PreviewNaming(ctx, v.ID, accepted.Version, &b)
				if err != nil || newPreview.Digest == preview.Digest {
					t.Fatal(err)
				}
				if err = f.engine.ConfirmNaming(ctx, v.ID, accepted.Version, preview.Digest); err == nil {
					t.Fatal("old preview approved")
				}
				return
			}
			if name == "manual_provenance" {
				if err = f.engine.RejectNaming(ctx, v.ID, accepted.Version); err != nil {
					t.Fatal(err)
				}
				manual, err := f.engine.BatchPreview(ctx, f.job().ID, f.job().Version, *preview.Plan.Batch)
				if err != nil || manual.Plan.Assistance == nil {
					t.Fatal("lost exposure")
				}
				if err = f.engine.BatchConfirm(ctx, f.job().ID, manual.JobVersion, manual.Plan.Version, manual.Digest, *manual.Plan.Batch); err != nil {
					t.Fatal("rejected model locked manual fallback", err)
				}
				f.mu.Lock()
				f.discovered = true
				f.mu.Unlock()
				f.tick(1001)
				original, _, err := f.store.namingReview(ctx, v.ID)
				if err != nil || original.State != "rejected" {
					t.Fatal("manual fallback rewrote model rejection")
				}
				page, err := f.store.LearningSamples(ctx, 0)
				if err != nil || len(page.Items) != 0 || comparisons(t, f)[0].Intent.Origin != "model_assisted_human" {
					t.Fatal("manual fallback forged human evidence")
				}
				return
			}
			if name == "conflict" {
				target := filepath.Join(f.library, preview.Plan.Files[0].Target)
				if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(target, []byte("other"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if name == "rollback" {
				if _, err = f.store.db.Exec(`CREATE TRIGGER fixture_fail_compare BEFORE INSERT ON comparison_records BEGIN SELECT RAISE(ABORT,'fixture'); END;`); err != nil {
					t.Fatal(err)
				}
			}
			if err = f.engine.ConfirmNaming(ctx, v.ID, accepted.Version, preview.Digest); err == nil {
				t.Fatal("conflict/rollback approved")
			}
			persisted, _, err := f.store.namingReview(ctx, v.ID)
			if err != nil || persisted.State != "suggested" || f.job().Import != "needs_confirmation" {
				t.Fatal("non-atomic approval", err)
			}
		})
	}
}
func TestNamingServiceIsolation(t *testing.T) {
	f := namingFixture(t)
	_, web := managementFixture(t, f)
	server := httptest.NewUnstartedServer(nil)
	server.Config.Handler = LearningHandler(f.store, learningFixtureToken, server.Listener.Addr().String(), f.engine)
	server.Start()
	defer server.Close()
	// 未请求任务根本没有导出；服务不能按任意job读取详情或进入审核。
	for _, route := range []string{"/v1/naming/requests?after=0", "/v1/naming/jobs?job_id=" + f.job().ID, "/api/naming-confirm"} {
		req, _ := http.NewRequest("GET", server.URL+route, nil)
		req.Header.Set("Authorization", "Bearer "+learningFixtureToken)
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if route == "/v1/naming/requests?after=0" {
			var page struct {
				Items []any `json:"items"`
			}
			if json.NewDecoder(res.Body).Decode(&page) != nil || len(page.Items) != 0 {
				t.Fatal("unauthorized job exported")
			}
		} else if res.StatusCode != 404 {
			t.Fatal("unscoped access")
		}
		res.Body.Close()
	}
	m, web2 := managementFixture(t, f)
	defer web2.Close()
	for _, route := range []string{"batch-confirm", "job-action", "naming-confirm", "proposal-approve"} {
		req, _ := http.NewRequest("POST", web2.URL+"/api/"+route, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+learningFixtureToken)
		req.Header.Set("Origin", web2.URL)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Nonce", m.csrf)
		res, err := web2.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Fatal("service bypass via management", route)
		}
	}

	req, _ := http.NewRequest("POST", web2.URL+"/api/naming-confirm", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+learningFixtureToken)
	req.Header.Set("Origin", web2.URL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Nonce", m.csrf)
	res, err := web2.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("service self approval")
	}
	_ = web
	v := namingRequest(t, f)
	body, _ := json.Marshal(v.Snapshot)
	for _, secret := range []string{f.downloads, "magnet:", "private-guid", "fixture-only-token", "https:"} {
		if strings.Contains(string(body), secret) {
			t.Fatal(fmt.Sprint("private snapshot"))
		}
	}
}

// 人工独立修订仍可完成单文件，但必须留下模型接触标记，不能伪造成纯人工学习样本。
func TestNamingLegacyManualRetainsExposure(t *testing.T) {
	ctx := context.Background()
	f := namingFixture(t)
	v := namingRequest(t, f)
	if _, err := f.engine.SubmitNaming(ctx, namingResult(v)); err != nil {
		t.Fatal(err)
	}
	if err := f.engine.RejectNaming(ctx, v.ID, 2); err != nil {
		t.Fatal(err)
	}
	plan, err := f.engine.Propose(ctx, f.job().ID, f.name, "Example/Season 03/Example - S03E01.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Assistance == nil || plan.Assistance.ID != v.ID {
		t.Fatal("legacy lost model source")
	}
	if err = f.engine.Confirm(ctx, f.job().ID, plan.Version, PlanDigest(plan)); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	f.tick(1001)
	records := comparisons(t, f)
	if len(records) != 1 || records[0].Intent.Origin != "model_assisted_human" {
		t.Fatal("legacy forged evidence")
	}
	page, err := f.store.LearningSamples(ctx, 0)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("legacy learning loop")
	}
}

// 未部署schema7升级只增加辅助表，旧任务、计划与不可变人工依据必须逐字保留。
func TestNamingSchemaUpgradePreservesEvidence(t *testing.T) {
	f, _, _ := learningFixture(t)
	ctx := context.Background()
	before := digestStringJSON(comparisons(t, f)[0].Intent)
	jobBefore := digestStringJSON(f.job())
	if _, err := f.store.db.Exec(`DROP TABLE naming_assistance_audit;DROP TABLE naming_requests;PRAGMA user_version=7`); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	if digestStringJSON(comparisons(t, f)[0].Intent) != before || digestStringJSON(f.job()) != jobBefore {
		t.Fatal("migration rewrote evidence")
	}
	var version int
	if err := f.store.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 8 {
		t.Fatal("schema not upgraded", err)
	}
	page, err := f.store.LearningSamples(ctx, 0)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("pure human learning lost")
	}
}

// pending模型请求不参与做种计时；正交状态推进复用原请求/结果，不花新额度。
func TestNamingOfflineDoesNotHoldSeedDeadline(t *testing.T) {
	ctx := context.Background()
	f := namingFixture(t)
	v := namingRequest(t, f)
	f.tick(f.job().StopAt)
	j := f.job()
	if j.Seed != "stopped" || j.Import != "needs_confirmation" || j.Version == v.Snapshot.JobVersion {
		t.Fatal("fixture did not advance seeding")
	}
	repeated, err := f.engine.RequestNaming(ctx, j.ID, j.Version, v.Snapshot.Hints, false)
	if err != nil || repeated.ID != v.ID {
		t.Fatal("seed progress consumed request quota", err)
	}
	accepted, err := f.engine.SubmitNaming(ctx, namingResult(v))
	if err != nil {
		t.Fatal("seed progress invalidated result", err)
	}
	preview, err := f.engine.PreviewNaming(ctx, accepted.ID, accepted.Version, nil)
	if err != nil || preview.JobVersion != j.Version {
		t.Fatal("preview not bound to current job", err)
	}
	if err = f.engine.ConfirmNaming(ctx, accepted.ID, accepted.Version, preview.Digest); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	f.tick(j.StopAt + 1)
	if f.job().Seed != "stopped" || f.job().Import != "imported" || f.job().Discovery != "confirmed" {
		t.Fatal("same offline task did not resume")
	}
	var requests int
	if err = f.store.db.QueryRow(`SELECT count(*) FROM naming_requests`).Scan(&requests); err != nil || requests != 1 {
		t.Fatal("recharged request quota", requests, err)
	}
}
