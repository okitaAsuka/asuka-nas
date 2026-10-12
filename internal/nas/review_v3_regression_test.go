package nas

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCompleteNumericTokensRejectUnsupportedSemantics(t *testing.T) {
	for _, prefix := range []string{"Example - ", "Example S09E", "Example [总第"} {
		for _, number := range []string{"02.5", "01-02.5", "01~02/3", "02½", "02⅓", "02²", "02&#189;", "02⁄3", "01-02v2"} {
			if _, e := ParseTitle(prefix + number); e == nil {
				t.Errorf("unsupported full token accepted: %s%s", prefix, number)
			}
		}
	}
	for _, title := range []string{"Example [02] [720p]", "Example S09E02.mkv", "Example - 01~02", "Example【０２】", "Example - 02 [2026]"} {
		if _, e := ParseTitle(title); e != nil {
			t.Errorf("supported control %s: %v", title, e)
		}
	}
}
func TestSpecialKindGateUniformAcrossContextsAndFiles(t *testing.T) {
	for _, special := range []string{"SP", "OVA", "OAD", "NCOP", "NCED", "PV", "Special", "特典", "特别篇", "番外"} {
		for _, bounded := range []bool{false, true} {
			r := reviewRule()
			if bounded {
				r.Episodes = &EpisodeRange{1, 11}
			}
			for _, position := range []string{"rss", "file"} {
				title := "Example [02]"
				source := "Example [02].mkv"
				if position == "rss" {
					title += " [" + special + "]"
				} else {
					source = "Example [" + special + "] [02].mkv"
				}
				if _, e := r.Resolve(title, source); e == nil {
					t.Errorf("special bypass %s bounded=%v position=%s", special, bounded, position)
				}
			}
		}
	}
}
func TestDisabledAliasBoundaryClasses(t *testing.T) {
	f := newFixture(t, "Example.S09E02.mkv", -1)
	r := reviewRule()
	installRule(t, f.store, r)
	if e := f.store.SetRule(context.Background(), r.ID, 1, false); e != nil {
		t.Fatal(e)
	}
	for i, title := range []string{"Example: S09E02", "Example：S09E02", "Example【02】", "Example（02）", "Example(02)", "[Example][02]", "Example〔02〕", "Example「02」"} {
		p, e := ParseTitle(title)
		if e != nil || !r.matches("fixture", p) {
			t.Fatalf("alias boundary %d: %s %v", i, title, e)
		}
	}
}
func TestLegacyEvidenceBridgePreservesPlanAndBlocksUnproven(t *testing.T) {
	for _, mode := range []string{"proven", "empty", "contradiction", "added_after_plan"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, "Example.S09E02.mkv", -1)
			title := "Example S09E02"
			if mode == "empty" {
				title = ""
			}
			addRelease(t, f, "fixture", "one", title)
			f.tick(1000)
			j := f.job()
			j.Plan.Manual = nil
			j.Plan.Evidence = nil
			j.Plan.ParserVersion = 1
			if mode == "contradiction" {
				j.Plan.Files[0].Target = "Example/Season 08/Example - S08E03.mkv"
			}
			before := PlanDigest(j.Plan)
			b, _ := json.Marshal(j)
			if _, e := f.store.db.Exec("UPDATE jobs SET body=? WHERE id=?", string(b), j.ID); e != nil {
				t.Fatal(e)
			}
			if mode == "added_after_plan" {
				addRelease(t, f, "fixture", "later", "Example S09E02")
			}
			reviewDowngradeV3(t, f)
			f.reopen()
			j = f.job()
			if PlanDigest(j.Plan) != before {
				t.Fatal("migration rewrote frozen plan")
			}
			if mode == "proven" {
				if j.BlockReason != "" {
					t.Fatal("proof not recovered")
				}
				addRelease(t, f, "fixture", "repeat", "Example S09E02")
				f.tick(1001)
				if f.job().BlockReason != "" {
					t.Fatal("same evidence blocked")
				}
				addRelease(t, f, "fixture", "conflict", "Example S08E03")
				f.tick(1002)
				if f.job().BlockReason != "late_mapping_conflict" {
					t.Fatal("new contradiction ignored")
				}
			} else {
				if j.BlockReason != "legacy_evidence_unresolved" {
					t.Fatal("unproven plan guessed", j.BlockReason)
				}
				f.tick(1001)
				if f.job().Discovery == "confirmed" {
					t.Fatal("unproven became ready")
				}
			}
		})
	}
}

func TestSpecialKindInAnyManifestItemBlocks(t *testing.T) {
	for _, extra := range []string{"Example [02].SP.ass", "OVA-info.nfo", "SP/notes.txt"} {
		files := []string{"Example [02].mkv", extra}
		if _, reason := singleVideo(files); reason != "unsupported_episode_kind" {
			t.Fatal(extra, reason)
		}
		if _, _, e := manifestGroups(files); e == nil || e.Error() != "unsupported_episode_kind" {
			t.Fatal(extra, e)
		}
	}
}
