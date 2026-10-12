package nas

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestIndependentV4DelimiterAcceptance(t *testing.T) {
	for _, marker := range []string{"sp03", "ＯＶＡ", "NCOP01"} {
		for _, sep := range []string{"_", "-", "—", "+"} {
			t.Run(marker+sep, func(t *testing.T) {
				f := newFixture(t, "Example"+sep+marker+sep+"S09E02.mkv", -1)
				installRule(t, f.store, reviewRule())
				addRelease(t, f, "fixture", "one", "Example [02]")
				f.tick(1000)
				if j := f.job(); j.Import != "needs_confirmation" || j.BlockReason != "unsupported_episode_kind" {
					t.Fatalf("marker escaped: %s %s", j.Import, j.BlockReason)
				}
			})
		}
	}
	for _, word := range []string{"SPARK", "NOVATION", "OVATION"} {
		t.Run(word, func(t *testing.T) {
			f := newFixture(t, word+".S09E02.mkv", -1)
			r := reviewRule()
			r.Aliases = []string{word}
			installRule(t, f.store, r)
			addRelease(t, f, "fixture", "one", word+" [02]")
			f.tick(1000)
			if j := f.job(); j.Import != "imported" {
				t.Fatalf("embedded ordinary word blocked: %s", j.BlockReason)
			}
		})
	}
}

func TestIndependentV4LegacyGroupAliasAndFrozenRawPaths(t *testing.T) {
	ctx := context.Background()
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			f := newFixture(t, "Example.S09E02.mkv", -1)
			r := reviewRule()
			installRule(t, f.store, r)
			if !enabled {
				if err := f.store.SetRule(ctx, r.ID, 1, false); err != nil {
					t.Fatal(err)
				}
			}
			// 旧规则的合法原文：圆括号alias、普通&字幕组、带圆括号目录。只写fixture数据库。
			r.IdentityNormalizationVersion = 0
			r.Aliases = []string{"Example (2026)"}
			r.Group = "Team & One"
			r.Directory = "Archive (Raw)"
			raw, _ := json.Marshal(r)
			if _, err := f.store.db.Exec("UPDATE naming_rules SET body=? WHERE id=?", string(raw), r.ID); err != nil {
				t.Fatal(err)
			}
			addRelease(t, f, "fixture", "one", "[Team &amp; One] Example【2026】 [02]")
			f.tick(1000)
			j := f.job()
			if enabled {
				if j.Import != "imported" || !strings.HasPrefix(j.Plan.Files[0].Target, "Archive (Raw)/") {
					t.Fatalf("legacy match/path regression: %s %s", j.Import, j.BlockReason)
				}
				digest := PlanDigest(j.Plan)
				addRelease(t, f, "fixture", "same-semantics", "[Team & One] Example_（2026） [02]")
				f.reopen()
				f.tick(1001)
				j = f.job()
				if j.BlockReason != "" || PlanDigest(j.Plan) != digest {
					t.Fatal("frozen digest or compatible evidence regressed", j.BlockReason)
				}
			} else if j.BlockReason != "rule_disabled" || j.Import != "needs_confirmation" {
				t.Fatal("disabled legacy identity escaped", j.Import, j.BlockReason)
			}
			var stored string
			if err := f.store.db.QueryRow("SELECT body FROM naming_rules WHERE id=?", r.ID).Scan(&stored); err != nil || stored != string(raw) {
				t.Fatal("raw rule mutated", err)
			}
		})
	}
}

func TestIndependentV4UnknownLegacyVersionBlocksFallback(t *testing.T) {
	f := newFixture(t, "Example.S09E02.mkv", -1)
	r := reviewRule()
	installRule(t, f.store, r)
	r.IdentityNormalizationVersion = 2
	r.Aliases = []string{"Example (2026)"}
	raw, _ := json.Marshal(r)
	if _, err := f.store.db.Exec("UPDATE naming_rules SET body=? WHERE id=?", string(raw), r.ID); err != nil {
		t.Fatal(err)
	}
	addRelease(t, f, "fixture", "one", "Example (2026) [02]")
	f.tick(1000)
	if j := f.job(); j.BlockReason != "rule_normalization_unresolved" || j.Import != "needs_confirmation" {
		t.Fatal("unknown version fell back", j.Import, j.BlockReason)
	}
}
