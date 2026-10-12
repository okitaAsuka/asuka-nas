package nas

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func reviewRule() NamingRule {
	return NamingRule{ID: "review", Revision: 1, SeriesID: "review-series", Provider: "manual", Order: "confirmed", Subscription: "fixture", Aliases: []string{"Example"}, Season: 9, Directory: "Confirmed", DisplayName: "Example"}
}

func TestIndependentDisabledRuleFormatting(t *testing.T) {
	for _, title := range []string{"[Example] [02]", "Example S09E02", "Example [02]"} {
		t.Run(title, func(t *testing.T) {
			f := newFixture(t, "Example.S09E02.mkv", -1)
			r := reviewRule()
			installRule(t, f.store, r)
			if err := f.store.SetRule(context.Background(), r.ID, 1, false); err != nil {
				t.Fatal(err)
			}
			addRelease(t, f, "fixture", "one", title)
			f.tick(1000)
			j := f.job()
			if j.Import != "needs_confirmation" || j.BlockReason != "rule_disabled" {
				t.Fatalf("disabled rule bypassed: import=%s block=%q target=%s", j.Import, j.BlockReason, j.Plan.Files[0].Target)
			}
		})
	}
}

func TestIndependentFrozenRuleRevisionWithIdenticalEvidence(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "Example [02].mkv", -1)
	r := reviewRule()
	installRule(t, f.store, r)
	addRelease(t, f, "fixture", "one", "Example [02]")
	f.tick(1000)
	before := f.job()
	if before.Import != "imported" {
		t.Fatal(before.Import)
	}
	r.Revision = 2
	r.Directory = "FutureOnly"
	installRule(t, f.store, r)
	// 同一订阅另一个 GUID 发布完全相同的内容；没有新的矛盾。
	addRelease(t, f, "fixture", "duplicate-guid", "Example [02]")
	f.reopen()
	if err := f.engine.RetryTick(ctx, time.Unix(1001, 0), 1); err != nil {
		t.Fatal(err)
	}
	after := f.job()
	if after.BlockReason != "" {
		t.Fatalf("unchanged evidence checked against new rules: block=%s discovery=%s", after.BlockReason, after.Discovery)
	}
}

func TestIndependentManualFrozenLateEpisodeConflict(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "unknown.mkv", -1)
	id := addRelease(t, f, "fixture", "one", "")
	f.tick(1000)
	p, err := f.engine.Propose(ctx, id, f.name, "Example/Season 01/Example - S01E02.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.engine.Confirm(ctx, id, p.Version, PlanDigest(p)); err != nil {
		t.Fatal(err)
	}
	f.tick(1001)
	addRelease(t, f, "fixture", "late", "Example S02E03")
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	f.tick(1002)
	j := f.job()
	if j.BlockReason != "late_mapping_conflict" {
		t.Fatalf("contradictory late evidence ignored: block=%q discovery=%s target=%s", j.BlockReason, j.Discovery, j.Plan.Files[0].Target)
	}
}

func TestIndependentDecimalEpisode(t *testing.T) {
	f := newFixture(t, "Example - 02.5.mkv", -1)
	installRule(t, f.store, reviewRule())
	addRelease(t, f, "fixture", "one", "Example - 02.5")
	f.tick(1000)
	j := f.job()
	if j.Import == "imported" {
		t.Fatalf("special episode 2.5 imported as ordinary episode: target=%s", j.Plan.Files[0].Target)
	}
}

func TestIndependentAbsoluteEvidenceConflict(t *testing.T) {
	f := newFixture(t, "Example [02] [总第68].mkv", -1)
	installRule(t, f.store, reviewRule())
	addRelease(t, f, "fixture", "one", "Example [02] [总第67]")
	f.tick(1000)
	j := f.job()
	if j.Import == "imported" {
		t.Fatalf("contradictory absolute evidence ignored: target=%s", j.Plan.Files[0].Target)
	}
}

func TestIndependentControls(t *testing.T) {
	t.Run("html_unicode_and_hardlink", func(t *testing.T) {
		f := newFixture(t, "Example ［０２］.mkv", -1)
		installRule(t, f.store, reviewRule())
		addRelease(t, f, "fixture", "one", "Example&nbsp;［０２］ [2026]")
		f.tick(1000)
		j := f.job()
		if j.Import != "imported" || j.Plan.Decision.Episode != 2 {
			t.Fatal(j)
		}
		a, e := os.Stat(filepath.Join(f.downloads, f.name))
		if e != nil {
			t.Fatal(e)
		}
		b, e := os.Stat(filepath.Join(f.library, j.Plan.Files[0].Target))
		if e != nil || !os.SameFile(a, b) {
			t.Fatal("not a hardlink", e)
		}
		if j.Seed != "stopped" || j.Discovery == "confirmed" {
			t.Fatal("stop/Emby separation", j)
		}
	})
	t.Run("source_conflict_survives_restart", func(t *testing.T) {
		f := newFixture(t, "Example [02].mkv", -1)
		installRule(t, f.store, reviewRule())
		addRelease(t, f, "fixture", "one", "Example [02]")
		addRelease(t, f, "fixture", "one", "Example [03]")
		f.reopen()
		f.tick(1000)
		j := f.job()
		if j.BlockReason != "title_file_conflict" || j.Import != "needs_confirmation" {
			t.Fatal(j)
		}
	})
}

func TestIndependentScopeAndVersionControls(t *testing.T) {
	ctx := context.Background()
	t.Run("overlapping_rules_do_not_gain_implicit_priority", func(t *testing.T) {
		f := newFixture(t, "Example [02].mkv", -1)
		broad := reviewRule()
		installRule(t, f.store, broad)
		specific := reviewRule()
		specific.ID = "specific"
		specific.Group = "Group"
		specific.Directory = "Different"
		installRule(t, f.store, specific)
		addRelease(t, f, "fixture", "one", "[Group] Example [02]")
		f.tick(1000)
		if j := f.job(); j.Import != "needs_confirmation" || j.BlockReason != "mapping_conflict" {
			t.Fatal(j)
		}
	})
	t.Run("revision_immutable_draft_not_enabled", func(t *testing.T) {
		f := newFixture(t, "Example [02].mkv", -1)
		r := reviewRule()
		if err := f.store.AddRule(ctx, r); err != nil {
			t.Fatal(err)
		}
		changed := r
		changed.Directory = "Changed"
		if err := f.store.AddRule(ctx, changed); err == nil {
			t.Fatal("mutable revision")
		}
		got, err := f.store.Rule(ctx, r.ID, 1)
		if err != nil || got.Directory != r.Directory {
			t.Fatal(got, err)
		}
		addRelease(t, f, "fixture", "one", "Example [02]")
		f.tick(1000)
		if j := f.job(); j.Import != "needs_confirmation" {
			t.Fatal("draft authorized import", j)
		}
	})
}
