package nas

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func manualRule() NamingRule {
	return NamingRule{ID: "fixture-rule", Revision: 1, SeriesID: "fixture-series", Provider: "manual", Order: "fixture-custom", Subscription: "fixture", Group: "测试组", Aliases: []string{"虚构作品 篇章"}, Season: 9, Directory: "用户选择目录", DisplayName: "虚构作品"}
}
func installRule(t *testing.T, s *Store, r NamingRule) {
	t.Helper()
	if e := s.AddRule(context.Background(), r); e != nil {
		t.Fatal(e)
	}
	if e := s.SetRule(context.Background(), r.ID, r.Revision, true); e != nil {
		t.Fatal(e)
	}
}
func addRelease(t *testing.T, f *fixture, sub, guid, title string) string {
	t.Helper()
	id, e := f.store.IngestRelease(context.Background(), sub, guid, f.hash, "magnet:?xt=urn:btih:"+f.hash, nil, 0, title)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func TestRuleTwoEpisodesAutomaticAndRestart(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "虚构作品 [02].mkv", -1)
	r := manualRule()
	installRule(t, f.store, r)
	addRelease(t, f, "fixture", "ep2", "[测试组] 虚构作品 篇章 [02] [1080p]")
	f.tick(1000)
	first := f.job()
	if first.Import != "imported" || !first.Plan.Started || first.Plan.Decision.Episode != 2 {
		t.Fatal("first episode did not import")
	}
	f.reopen()
	f.mu.Lock()
	f.hash = strings.Repeat("a", 40)
	f.name = "虚构作品 [03].mkv"
	f.mu.Unlock()
	if e := os.WriteFile(filepath.Join(f.downloads, f.name), []byte("episode 3"), 0600); e != nil {
		t.Fatal(e)
	}
	id := addRelease(t, f, "fixture", "ep3", "[测试组] 虚构作品 篇章 [03] [1080p]")
	f.tick(1001)
	second, e := f.store.Get(ctx, id)
	if e != nil || second.Import != "imported" || second.Plan.Decision.Episode != 3 {
		t.Fatal(second, e)
	}
	for _, j := range []Job{first, second} {
		p := j.Plan.Files[0]
		a, _ := os.Stat(filepath.Join(f.downloads, p.Source))
		b, e := os.Stat(filepath.Join(f.library, p.Target))
		if e != nil || !os.SameFile(a, b) {
			t.Fatal("not hardlinked")
		}
		if j.Plan.Decision.Season != 9 || j.Plan.Decision.Provider != "manual" {
			t.Fatal("manual identity lost")
		}
	}
}
func TestMappingConflictsBlockAndNeverFallback(t *testing.T) {
	for _, tc := range []struct {
		name, title, source, reason string
		second                      bool
	}{
		{"different_sources", "[测试组] 虚构作品 篇章 [02]", "Show.S09E02.mkv", "mapping_conflict", true},
		{"file_contradiction", "[测试组] 虚构作品 篇章 [03]", "Show.S09E02.mkv", "title_file_conflict", false},
		{"collection", "[测试组] 虚构作品 篇章 - 01~11 [总第67~77]", "Show.S09E02.mkv", "absolute_mapping_unresolved", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.source, -1)
			installRule(t, f.store, manualRule())
			addRelease(t, f, "fixture", "one", tc.title)
			if tc.second {
				r := manualRule()
				r.ID = "other"
				r.Subscription = "other"
				r.SeriesID = "other-series"
				r.Directory = "different"
				installRule(t, f.store, r)
				addRelease(t, f, "other", "two", tc.title)
			}
			f.tick(1000)
			j := f.job()
			if j.Import != "needs_confirmation" || j.BlockReason != tc.reason || j.Seed != "stopped" {
				t.Fatalf("%+v", j)
			}
			entries, _ := os.ReadDir(f.library)
			if len(entries) != 0 {
				t.Fatal("unsafe fallback")
			}
		})
	}
}
func TestLateConflictDoesNotMoveHistory(t *testing.T) {
	f := newFixture(t, "虚构作品 [02].mkv", -1)
	installRule(t, f.store, manualRule())
	title := "[测试组] 虚构作品 篇章 [02]"
	addRelease(t, f, "fixture", "one", title)
	f.tick(1000)
	before := f.job()
	r := manualRule()
	r.ID = "other"
	r.Subscription = "other"
	r.Directory = "elsewhere"
	installRule(t, f.store, r)
	addRelease(t, f, "other", "late", title)
	if e := f.engine.RetryTick(context.Background(), time.Unix(1001, 0), 30); e != nil {
		t.Fatal(e)
	}
	after := f.job()
	if after.Import != "imported" || after.BlockReason != "late_mapping_conflict" || PlanDigest(after.Plan) != PlanDigest(before.Plan) {
		t.Fatal("history rewritten or conflict lost")
	}
	if _, e := os.Stat(filepath.Join(f.library, before.Plan.Files[0].Target)); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(f.library, "elsewhere")); !os.IsNotExist(e) {
		t.Fatal("new target executed")
	}
}
func TestRuleVersionInvalidatesOnlyUnstartedPlans(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(fmt.Sprint(started), func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t, "虚构作品 [02].mkv", -1)
			r := manualRule()
			installRule(t, f.store, r)
			addRelease(t, f, "fixture", "one", "[测试组] 虚构作品 篇章 [02]")
			j := f.job()
			j.Download = "completed"
			j.CompletedAt = 1000
			j.StopAt = 1000
			j.Sources = []string{f.name}
			p, e := f.engine.namingPlan(ctx, &j, f.name)
			if e != nil {
				t.Fatal(e)
			}
			p.Approved = true
			j.Plan = p
			j.Import = "planned"
			if e = f.store.Save(ctx, &j, "plan_ready"); e != nil {
				t.Fatal(e)
			}
			if started {
				j.Plan.Started = true
				if e = f.store.Save(ctx, &j, "import_started"); e != nil {
					t.Fatal(e)
				}
				if e = f.files.Link(p.Files[0]); e != nil {
					t.Fatal(e)
				}
			}
			r.Revision = 2
			r.Directory = "new-directory"
			installRule(t, f.store, r)
			f.reopen()
			f.tick(1001)
			j = f.job()
			if started {
				if j.Import != "imported" || j.Plan.Files[0].Target != p.Files[0].Target {
					t.Fatal("frozen plan changed")
				}
				if e = f.store.Replan(ctx, j.ID); e == nil {
					t.Fatal("history replan accepted")
				}
			} else {
				if j.BlockReason != "stale_plan" || j.Plan.Started {
					t.Fatal("stale plan executed")
				}
				if e = f.engine.Confirm(ctx, j.ID, j.Plan.Version, PlanDigest(j.Plan)); e == nil {
					t.Fatal("stale confirmation")
				}
				if e = f.store.Replan(ctx, j.ID); e != nil {
					t.Fatal(e)
				}
				f.tick(1002)
				if !strings.HasPrefix(f.job().Plan.Files[0].Target, "new-directory/") {
					t.Fatal("explicit replan ignored")
				}
				var count int
				if e = f.store.db.QueryRow("SELECT COUNT(*) FROM plan_history WHERE job_id=?", j.ID).Scan(&count); e != nil || count != 2 {
					t.Fatal("plan history missing", count, e)
				}
			}
		})
	}
}
func TestTitlesBoundedImmutableAndRuleDisabled(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "Show.S09E02.mkv", -1)
	r := manualRule()
	installRule(t, f.store, r)
	title := "[测试组] 虚构作品 篇章 [02]"
	id := addRelease(t, f, "fixture", "one", title)
	before := f.job()
	addRelease(t, f, "fixture", "one", title)
	if f.job().Version != before.Version {
		t.Fatal("duplicate wrote state")
	}
	if e := f.store.AttachResourceTitle(ctx, "fixture", "one", strings.Repeat("x", TitleLimit+1)); e == nil {
		t.Fatal("unbounded title")
	}
	if e := f.store.SetRule(ctx, r.ID, 1, false); e != nil {
		t.Fatal(e)
	}
	f.tick(1000)
	if f.job().BlockReason != "rule_disabled" {
		t.Fatal("disabled fallback")
	}
	if e := f.store.SetRule(ctx, r.ID, 1, true); e != nil {
		t.Fatal(e)
	}
	if e := f.engine.RetryTick(ctx, time.Unix(1001, 0), 1); e != nil {
		t.Fatal(e)
	}
	if f.job().Import != "imported" {
		t.Fatal("enable did not wake")
	}
	if e := f.store.AttachResourceTitle(ctx, "fixture", "one", "[测试组] 虚构作品 篇章 [03]"); e != nil {
		t.Fatal(e)
	}
	contexts, e := f.store.Contexts(ctx, id)
	if e != nil || len(contexts) != 2 {
		t.Fatal("evidence overwritten", contexts, e)
	}
	f.tick(1002)
	if f.job().BlockReason != "late_mapping_conflict" {
		t.Fatal("changed title unnoticed")
	}
	events, e := f.store.Events(ctx, 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, event := range events {
		if strings.Contains(event.Kind, title) {
			t.Fatal("title in event")
		}
	}
}
func TestTitleEvidenceAndPureAbsoluteMapping(t *testing.T) {
	p, e := ParseTitle("[测试组][虚构作品第四季 / Fiction 4th - 01~11][总第67~77][1080p]")
	if e != nil || p.Season != 4 || p.Relative == nil || *p.Relative != (EpisodeRange{1, 11}) || p.Absolute == nil || *p.Absolute != (EpisodeRange{67, 77}) || !p.Collection {
		t.Fatal(p, e)
	}
	p, e = ParseTitle("[组] 东京复仇者 三天战争篇 / Tokyo Revengers:Santen Sensou [02] [2026][1080p]")
	if e != nil || p.Season != 0 || p.Relative.Start != 2 {
		t.Fatal("invented season", p, e)
	}
	p, e = ParseTitle("[组] 虚构作品 ［０２］ [2026][1080p]")
	if e != nil || p.Relative.Start != 2 {
		t.Fatal(p, e)
	}
	if _, e = ParseTitle("Show [02] - 03"); e == nil {
		t.Fatal("contradiction lost")
	}
	for _, a := range []int{66, 78} {
		if _, _, e = MapAbsolute(a, EpisodeRange{67, 77}, 4, 1); e == nil {
			t.Fatal("unbounded mapping")
		}
	}
	s, ep, e := MapAbsolute(77, EpisodeRange{67, 77}, 4, 1)
	if e != nil || s != 4 || ep != 11 {
		t.Fatal(s, ep, e)
	}
	r := manualRule()
	p, _ = ParseTitle("[测试组] 虚构作品 篇章外传 [02]")
	if r.matches("fixture", p) {
		t.Fatal("substring alias accepted")
	}
	if _, e = ParseTitle("Show\u202e [02]"); e == nil {
		t.Fatal("bidi accepted")
	}
}
func TestRuleImmutabilityAndSeriesOrder(t *testing.T) {
	f := newFixture(t, "Show.mkv", -1)
	r := manualRule()
	installRule(t, f.store, r)
	ctx := context.Background()
	if e := f.store.AddRule(ctx, r); e == nil {
		t.Fatal("revision overwritten")
	}
	r.Revision = 2
	r.Order = "different"
	if e := f.store.AddRule(ctx, r); e == nil {
		t.Fatal("series order mutated")
	}
	for _, dir := range []string{"../escape", "/absolute", "ok/../escape"} {
		r = manualRule()
		r.Directory = dir
		if r.Validate() == nil {
			t.Fatal("unsafe rule directory")
		}
	}
}

func TestStartPlanRejectsConcurrentRuleOrSourceChange(t *testing.T) {
	for _, change := range []string{"rule", "source"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t, "虚构作品 [02].mkv", -1)
			r := manualRule()
			installRule(t, f.store, r)
			addRelease(t, f, "fixture", "one", "[测试组] 虚构作品 篇章 [02]")
			j := f.job()
			j.Sources = []string{f.name}
			p, e := f.engine.namingPlan(ctx, &j, f.name)
			if e != nil {
				t.Fatal(e)
			}
			j.Plan = p
			j.Import = "planned"
			if e = f.store.Save(ctx, &j, "plan_ready"); e != nil {
				t.Fatal(e)
			}
			if change == "rule" {
				if e = f.store.SetRule(ctx, r.ID, 1, false); e != nil {
					t.Fatal(e)
				}
			} else {
				addRelease(t, f, "another", "two", "unknown [02]")
			}
			j.Plan.Started = true
			if e = f.store.Save(ctx, &j, "import_started"); e == nil {
				t.Fatal("concurrent change accepted")
			}
			entries, _ := os.ReadDir(f.library)
			if len(entries) != 0 {
				t.Fatal("file side effect")
			}
		})
	}
}
func TestNoTitleSourceCannotBeIgnored(t *testing.T) {
	f := newFixture(t, "Show.S09E02.mkv", -1)
	installRule(t, f.store, manualRule())
	addRelease(t, f, "fixture", "one", "[测试组] 虚构作品 篇章 [02]")
	addRelease(t, f, "other", "two", "")
	f.tick(1000)
	if f.job().BlockReason != "source_mapping_unresolved" {
		t.Fatal("unknown source ignored")
	}
}
func TestCollectionGateSurvivesRuleChangesWithoutWriteLoop(t *testing.T) {
	f := newFixture(t, "Show [02].mkv", -1)
	f.manifest = []string{f.name, "Show [03].mkv"}
	installRule(t, f.store, manualRule())
	addRelease(t, f, "fixture", "one", "[测试组] 虚构作品 篇章 [02]")
	f.tick(1000)
	before := f.job()
	f.tick(1001)
	after := f.job()
	if before.Version != after.Version || after.Import != "needs_confirmation" {
		t.Fatal("collection retried or imported")
	}
}

func TestContextAndRuleEventsAreAtomic(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "Show.mkv", -1)
	if _, e := f.store.db.Exec(`CREATE TRIGGER reject_context BEFORE INSERT ON events WHEN NEW.kind='source_context_added' BEGIN SELECT RAISE(ABORT,'fixture'); END`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.IngestRelease(ctx, "fixture", "one", fixtureHash, "magnet", nil, 0, "bounded title"); e == nil {
		t.Fatal("expected rollback")
	}
	ids, e := f.store.IDs(ctx)
	if e != nil || len(ids) != 0 {
		t.Fatal("partial ingest", ids, e)
	}
	r := manualRule()
	if e = f.store.AddRule(ctx, r); e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.db.Exec(`CREATE TRIGGER reject_rule BEFORE INSERT ON naming_events WHEN NEW.kind='rule_enabled' BEGIN SELECT RAISE(ABORT,'fixture'); END`); e != nil {
		t.Fatal(e)
	}
	if e = f.store.SetRule(ctx, r.ID, 1, true); e == nil {
		t.Fatal("expected rollback")
	}
	epoch, e := f.store.NamingEpoch(ctx)
	if e != nil || epoch != 0 {
		t.Fatal("partial activation", epoch, e)
	}
}
func TestMultiEpisodeAndZeroSeasonNeverBecomeSingle(t *testing.T) {
	for _, title := range []string{"Show S01E02E03", "Show S01E02-E03", "Show S01E02-03", "Show S00E02"} {
		if _, e := ParseTitle(title); e == nil {
			t.Fatal("ambiguous input accepted", title)
		}
	}
}
