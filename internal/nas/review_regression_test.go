package nas

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewDecimalVariantsAndIntegerExtensions(t *testing.T) {
	for _, title := range []string{"Example - 02.5", "Example S09E02.5", "Example [02.5]", "Example [总第67.5]", "Example - 02SP", "Example S09E02/03", "Example S09E02_03"} {
		t.Run(title, func(t *testing.T) {
			f := newFixture(t, strings.ReplaceAll(title, "/", "_")+".mkv", -1)
			installRule(t, f.store, reviewRule())
			addRelease(t, f, "fixture", "one", title)
			f.tick(1000)
			if f.job().Import != "needs_confirmation" {
				t.Fatal("unsupported token executed")
			}
			entries, _ := os.ReadDir(f.library)
			if len(entries) != 0 {
				t.Fatal("wrong ordinary episode")
			}
		})
	}
	for _, title := range []string{"Example - 02", "Example S09E02.mkv", "Example [02] [720p]", "Example [02] [480p]"} {
		t.Run(title, func(t *testing.T) {
			f := newFixture(t, "Example.S09E02.mkv", -1)
			installRule(t, f.store, reviewRule())
			addRelease(t, f, "fixture", "one", title)
			f.tick(1000)
			if f.job().Import != "imported" {
				t.Fatal("integer regressed", f.job().BlockReason)
			}
		})
	}
}
func TestReviewKnownIdentityUncertainScopeBlocksFallback(t *testing.T) {
	for _, title := range []string{"Example unspecified-format S09E02", "[OtherGroup] Example [02]"} {
		t.Run(title, func(t *testing.T) {
			f := newFixture(t, "Example.S09E02.mkv", -1)
			r := reviewRule()
			r.Group = "ExpectedGroup"
			installRule(t, f.store, r)
			addRelease(t, f, "fixture", "one", title)
			f.tick(1000)
			if f.job().BlockReason != "rule_scope_ambiguous" {
				t.Fatal("uncertain known rule bypass", f.job().BlockReason)
			}
		})
	}
}
func TestReviewFrozenCollectionNewGUIDIgnoresFutureRule(t *testing.T) {
	f := packFixture(t, "mixed")
	f.engine.Files = &faultFiles{FileOperations: f.files, at: 5, permission: true}
	if e := f.engine.Tick(context.Background(), time.Unix(1000, 0)); e == nil {
		t.Fatal("expected failure")
	}
	before := f.job()
	digest := PlanDigest(before.Plan)
	r := packRule()
	r.Revision = 2
	r.Directory = "future"
	installRule(t, f.store, r)
	addRelease(t, f, "fixture", "identical-new-guid", packTitle)
	f.reopen()
	f.tick(1001)
	j := f.job()
	if j.Import != "imported" || j.BlockReason != "" || PlanDigest(j.Plan) != digest {
		t.Fatal("identical evidence broke frozen recovery", j.BlockReason)
	}
	if _, e := os.Stat(filepath.Join(f.library, "future")); !os.IsNotExist(e) {
		t.Fatal("history migrated")
	}
}
func TestReviewManualSemanticAndOpaqueLateEvidence(t *testing.T) {
	for _, target := range []string{"Example/Season 01/Example - S01E02.mkv", "Chosen.mkv"} {
		t.Run(target, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t, "unknown.mkv", -1)
			id := addRelease(t, f, "fixture", "one", "")
			f.tick(1000)
			p, e := f.engine.Propose(ctx, id, f.name, target)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.engine.Confirm(ctx, id, p.Version, PlanDigest(p)); e != nil {
				t.Fatal(e)
			}
			f.tick(1001)
			addRelease(t, f, "fixture", "repeat", "")
			f.tick(1002)
			if f.job().BlockReason != "" {
				t.Fatal("identical empty evidence blocked")
			}
			addRelease(t, f, "fixture", "late", "Other S01E02")
			f.tick(1003)
			if f.job().BlockReason != "late_mapping_conflict" {
				t.Fatal("identity/opaque semantics ignored")
			}
		})
	}
}
func TestReviewFrozenRuleStillRejectsGenuineNewEvidence(t *testing.T) {
	f := newFixture(t, "Example [02].mkv", -1)
	r := reviewRule()
	installRule(t, f.store, r)
	addRelease(t, f, "fixture", "one", "Example [02]")
	f.tick(1000)
	r.Revision = 2
	r.Directory = "FutureOnly"
	installRule(t, f.store, r)
	addRelease(t, f, "fixture", "changed", "Example [03]")
	f.reopen()
	f.tick(1001)
	if f.job().BlockReason != "late_mapping_conflict" {
		t.Fatal("real contradiction hidden")
	}
}

func TestReviewOldFalseConflictCanClearWithoutMoving(t *testing.T) {
	f := newFixture(t, "Example [02].mkv", -1)
	r := reviewRule()
	installRule(t, f.store, r)
	addRelease(t, f, "fixture", "one", "Example [02]")
	f.tick(1000)
	r.Revision = 2
	r.Directory = "FutureOnly"
	installRule(t, f.store, r)
	addRelease(t, f, "fixture", "repeat", "Example [02]")
	j := f.job()
	digest := PlanDigest(j.Plan)
	j.CheckedContext = j.ContextVersion
	j.BlockReason = "late_mapping_conflict"
	j.ErrorCode = j.BlockReason
	if e := f.store.Save(context.Background(), &j, "late_mapping_conflict"); e != nil {
		t.Fatal(e)
	}
	f.reopen()
	f.tick(1001)
	j = f.job()
	if j.BlockReason != "" || PlanDigest(j.Plan) != digest {
		t.Fatal("false conflict did not clear safely")
	}
}
func TestRelativePriorityStillChecksExplicitAbsolute(t *testing.T) {
	r := packRule()
	r.Absolute = &AbsoluteMapping{1, 11, 5}
	r.Episodes = &EpisodeRange{5, 15}
	title := "[测试组] 虚构作品第四季 - 05~15 [总第1~11]"
	d, e := r.Resolve(title, "Show [05].mkv")
	if e != nil || d.Episode != 5 {
		t.Fatal("relative priority lost", d, e)
	}
	if _, e = r.Resolve(title, "Show [05] [总第5].mkv"); e == nil {
		t.Fatal("explicit absolute conflict ignored")
	}
}
