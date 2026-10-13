package nas

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func namingDaemon(f *fixture, automatic bool) *Daemon {
	return &Daemon{Engine: f.engine, Config: Config{TickSeconds: 15, Learning: LearningServiceConfig{Enabled: true, AutomaticNaming: automatic}}}
}
func requestCount(t *testing.T, f *fixture) int {
	t.Helper()
	var n int
	if err := f.store.db.QueryRow(`SELECT count(*) FROM naming_requests`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestNamingAutomaticOptInAndSemanticOnce(t *testing.T) {
	ctx := context.Background()
	f := namingFixture(t)
	d := namingDaemon(f, false)
	if err := d.TickJobs(ctx, time.Unix(1001, 0)); err != nil {
		t.Fatal(err)
	}
	if requestCount(t, f) != 0 {
		t.Fatal("automatic enabled by default")
	}
	d.Config.Learning.AutomaticNaming = true
	if err := d.TickJobs(ctx, time.Unix(1002, 0)); err != nil {
		t.Fatal(err)
	}
	items, err := f.engine.NamingReviews(ctx, f.job().ID)
	if err != nil || len(items) != 1 || items[0].Snapshot.Trigger != "automatic" || items[0].Snapshot.SemanticDigest == "" || items[0].Snapshot.Hints != (NamingHints{}) {
		t.Fatal("automatic request missing/fabricated metadata", err)
	}
	v := items[0]
	for i := 0; i < 3; i++ {
		if err = d.TickJobs(ctx, time.Unix(1003+int64(i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	d.TickJobs(ctx, time.Unix(f.job().StopAt, 0))
	if requestCount(t, f) != 1 || f.job().Seed != "stopped" {
		t.Fatal("seeding created new request")
	}
	// 自动无资料结果只能未解；重启和拒绝都不能重新推理。
	r := NamingResult{Protocol: 1, ID: v.ID, Digest: v.Snapshot.Digest, Status: "unresolved", Explanation: "缺少明确目录和季号", Uncertainties: []string{"人工需补充事实"}, Files: []NamingSuggestionRow{{FileID: v.Snapshot.Files[0].ID, Basis: "仅有文件名", Uncertainties: []string{"季号"}}}}
	accepted, err := f.engine.SubmitNaming(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	f.reopen()
	d.Engine = f.engine
	if err = d.TickJobs(ctx, time.Unix(f.job().StopAt+1, 0)); err != nil {
		t.Fatal(err)
	}
	if requestCount(t, f) != 1 {
		t.Fatal("unresolved auto repeat")
	}
	if err = f.engine.RejectNaming(ctx, v.ID, accepted.Version); err != nil {
		t.Fatal(err)
	}
	if err = d.TickJobs(ctx, time.Unix(f.job().StopAt+2, 0)); err != nil {
		t.Fatal(err)
	}
	if requestCount(t, f) != 1 {
		t.Fatal("rejection auto repeat")
	}
	// 即使新增来源，拒绝终态也不由自动队列重新猜测。
	if err = f.store.AttachResourceTitle(ctx, "fixture", "private-guid", "Example additional [01]"); err != nil {
		t.Fatal(err)
	}
	if err = d.TickJobs(ctx, time.Unix(f.job().StopAt+3, 0)); err != nil {
		t.Fatal(err)
	}
	if requestCount(t, f) != 1 {
		t.Fatal("rejected task auto retried on new context")
	}
	// 人工facts和显式retry是唯一补充入口；同语义的人工版本不会触发另一自动请求。
	next, err := f.engine.RequestNaming(ctx, f.job().ID, f.job().Version, NamingHints{"Example", "Example", 2}, true)
	if err != nil || next.Snapshot.Trigger != "human" || next.Snapshot.SemanticDigest == v.Snapshot.SemanticDigest {
		t.Fatal("human supplement", err)
	}
	if err = d.TickJobs(ctx, time.Unix(f.job().StopAt+3, 0)); err != nil {
		t.Fatal(err)
	}
	if requestCount(t, f) != 2 {
		t.Fatal("human supplement auto duplicated")
	}
}
func TestNamingAutomaticSupportedStatesAndBoundedPage(t *testing.T) {
	for _, name := range []string{"special", "unsupported", "rules", "manual_plan", "page_limit", "bad_input_cursor", "pending_supplement"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := namingFixture(t)
			d := namingDaemon(f, true)
			j := f.job()
			switch name {
			case "special":
				j.BlockReason = "unsupported_episode_kind"
			case "unsupported":
				j.BlockReason = "unsupported_video"
			case "rules":
				r := manualRule()
				r.Group = ""
				r.Aliases = []string{"Example"}
				r.Season = 2
				installRule(t, f.store, r)
			case "manual_plan":
				p, err := f.engine.Propose(ctx, j.ID, f.name, "Example/Season 02/Example - S02E01.mkv")
				if err != nil || p == nil {
					t.Fatal(err)
				}
			case "page_limit", "bad_input_cursor":
				body, _ := json.Marshal(j)
				// 每个独立job均真实使用支持文件；任务标识/hash仍由NAS正常分配。
				for i := 0; i < 10; i++ {
					hash := strings.Repeat("f", 39) + string("0123456789"[i])
					id, err := f.store.IngestRelease(ctx, "fixture", hash, hash, "magnet:?xt=urn:btih:"+hash, nil, 7, "Example [01]")
					if err != nil {
						t.Fatal(err)
					}
					var clone Job
					json.Unmarshal(body, &clone)
					newJob, err := f.store.Get(ctx, id)
					if err != nil {
						t.Fatal(err)
					}
					clone.ID = id
					clone.Hash = hash
					clone.Version = newJob.Version
					if err = f.store.Save(ctx, &clone, "fixture_completed"); err != nil {
						t.Fatal(err)
					}
				}
				if name == "bad_input_cursor" {
					ids, err := f.store.IDs(ctx)
					if err != nil || len(ids) != 11 {
						t.Fatal(err)
					}
					for _, id := range ids[:8] {
						title := "https://fixture.invalid/private-feed"
						if _, err = f.store.db.Exec(`UPDATE release_context SET title=?,title_hash=? WHERE job_id=?`, title, digestString(title), id); err != nil {
							t.Fatal(err)
						}
					}
				}

			case "pending_supplement":
				if err := d.TickJobs(ctx, time.Unix(1001, 0)); err != nil {
					t.Fatal(err)
				}
				before, err := f.engine.NamingReviews(ctx, j.ID)
				if err != nil || len(before) != 1 {
					t.Fatal(err)
				}
				added, err := f.engine.RequestNaming(ctx, j.ID, f.job().Version, NamingHints{"Example", "Example", 2}, true)
				if err != nil {
					t.Fatal(err)
				}
				old, _, err := f.store.namingReview(ctx, before[0].ID)
				if err != nil || old.State != "rejected" || added.State != "requested" {
					t.Fatal("pending auto request not superseded")
				}
				if _, err = f.engine.SubmitNaming(ctx, namingResult(before[0])); err == nil {
					t.Fatal("superseded late output accepted")
				}
			}
			if name == "special" || name == "unsupported" {
				if err := f.store.Save(ctx, &j, "fixture_state"); err != nil {
					t.Fatal(err)
				}
			}
			if err := d.TickJobs(ctx, time.Unix(1002, 0)); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "page_limit":
				if requestCount(t, f) != 8 {
					t.Fatal("unbounded automatic page")
				}
				if err := d.TickJobs(ctx, time.Unix(1003, 0)); err != nil {
					t.Fatal(err)
				}
				if requestCount(t, f) != 11 {
					t.Fatal("prior pages starved later jobs")
				}
			case "bad_input_cursor":
				if requestCount(t, f) != 0 {
					t.Fatal("unsafe context exported")
				}
				if err := d.TickJobs(ctx, time.Unix(1003, 0)); err != nil {
					t.Fatal(err)
				}
				if requestCount(t, f) != 3 {
					t.Fatal("bad input prefix starved safe later jobs")
				}

			case "pending_supplement":
				if requestCount(t, f) != 2 {
					t.Fatal("new auto after supplement")
				}
			default:
				if requestCount(t, f) != 0 {
					t.Fatal("unsupported/solved/manually planned job queued")
				}
			}
		})
	}
}
func TestNamingSemanticChangesStillStale(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"manual_plan", "started", "manifest", "seed_between_preview_and_confirm"} {
		t.Run(name, func(t *testing.T) {
			f := namingFixture(t)
			v := namingRequest(t, f)
			if name == "seed_between_preview_and_confirm" {
				accepted, err := f.engine.SubmitNaming(ctx, namingResult(v))
				if err != nil {
					t.Fatal(err)
				}
				old, err := f.engine.PreviewNaming(ctx, v.ID, accepted.Version, nil)
				if err != nil {
					t.Fatal(err)
				}
				f.tick(f.job().StopAt)
				if err = f.engine.ConfirmNaming(ctx, v.ID, accepted.Version, old.Digest); err == nil {
					t.Fatal("obsolete preview approved")
				}
				fresh, err := f.engine.PreviewNaming(ctx, v.ID, accepted.Version, nil)
				if err != nil || fresh.Digest == old.Digest {
					t.Fatal("cannot refresh preview only", err)
				}
				if err = f.engine.ConfirmNaming(ctx, v.ID, accepted.Version, fresh.Digest); err != nil {
					t.Fatal(err)
				}
				if requestCount(t, f) != 1 {
					t.Fatal("preview refresh consumed new request")
				}
				return
			}
			j := f.job()
			switch name {
			case "manual_plan":
				if _, err := f.engine.Propose(ctx, j.ID, f.name, "Example/Season 02/Example - S02E01.mkv"); err != nil {
					t.Fatal(err)
				}
			case "started":
				j.Plan = &Plan{Version: j.Version + 1, Started: true}
				if err := f.store.Save(ctx, &j, "fixture_started"); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				j.Sources = nil
				if err := f.store.Save(ctx, &j, "fixture_manifest_changed"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.engine.SubmitNaming(ctx, namingResult(v)); err == nil {
				t.Fatal("real semantic change accepted")
			}
		})
	}
}
func TestNamingAutomaticConfigurationRequiresService(t *testing.T) {
	c := LearningServiceConfig{AutomaticNaming: true}
	if c.Validate() == nil {
		t.Fatal("automatic without service")
	}
	c.Enabled = true
	c.TokenEnv = "FIXTURE_NAMING_TOKEN"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
