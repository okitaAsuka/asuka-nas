package nas

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestReleaseTokenBoundaryContract(t *testing.T) {
	for _, marker := range []string{"SP", "OVA", "OAD", "NCOP", "NCED", "PV", "Special", "SP01", "OVA02"} {
		for _, pair := range [][2]string{{"_", "_"}, {"-", "-"}, {".", "."}, {" ", " "}, {"\t", "\t"}, {"[", "]"}, {"【", "】"}, {"(", ")"}, {"：", "："}, {"/", "/"}, {"+", "+"}} {
			s := "Example" + pair[0] + marker + pair[1] + "S09E02"
			if _, e := ParseTitle(s); e == nil {
				t.Errorf("title gate bypass: %q", s)
			}
			if e := releaseKind(s + ".mkv"); e == nil {
				t.Errorf("manifest gate bypass: %q", s)
			}
		}
	}
	for _, s := range []string{"Example.S09E02.mkv", "SPARK.S09E02.mkv", "NOVATION_S09E02.mkv", "OVATION [02]", "Space [02]"} {
		if e := releaseKind(s); e != nil {
			t.Errorf("embedded word blocked %s: %v", s, e)
		}
	}
}
func TestImmutableAliasComparisonCompatibility(t *testing.T) {
	ctx := context.Background()
	for _, alias := range []string{"Example (2026)", "Example（2026）", "Example [1080p]", "Example【2026】", "Example &amp;lt;"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%v", alias, enabled), func(t *testing.T) {
				f := newFixture(t, "Example.S09E02.mkv", -1)
				r := reviewRule()
				installRule(t, f.store, r)
				if !enabled {
					if e := f.store.SetRule(ctx, r.ID, 1, false); e != nil {
						t.Fatal(e)
					}
				}
				// 历史规则原文未标版本；比较不能更新此行。
				r.Aliases = []string{alias}
				raw, _ := json.Marshal(r)
				if _, e := f.store.db.Exec("UPDATE naming_rules SET body=? WHERE id=?", string(raw), r.ID); e != nil {
					t.Fatal(e)
				}
				addRelease(t, f, "fixture", "one", alias+" [02]")
				f.tick(1000)
				j := f.job()
				if enabled {
					if j.Import != "imported" || !strings.HasPrefix(j.Plan.Files[0].Target, "Confirmed/") {
						t.Fatal("enabled legacy identity lost", j.BlockReason)
					}
				} else if j.BlockReason != "rule_disabled" {
					t.Fatal("disabled legacy identity lost", j.BlockReason)
				}
				var after string
				if e := f.store.db.QueryRow("SELECT body FROM naming_rules WHERE id=?", r.ID).Scan(&after); e != nil || after != string(raw) {
					t.Fatal("immutable rule changed", e)
				}
				if enabled {
					digest := PlanDigest(j.Plan)
					addRelease(t, f, "fixture", "repeat", alias+" [02]")
					f.reopen()
					f.tick(1001)
					if PlanDigest(f.job().Plan) != digest || f.job().BlockReason != "" {
						t.Fatal("frozen history changed")
					}
				}
			})
		}
	}
}
func TestUnknownIdentityNormalizationFailsClosed(t *testing.T) {
	for _, bad := range []string{"future_version", "unsupported_numeric"} {
		t.Run(bad, func(t *testing.T) {
			f := newFixture(t, "Example.S09E02.mkv", -1)
			r := reviewRule()
			installRule(t, f.store, r)
			if bad == "future_version" {
				r.IdentityNormalizationVersion = 99
			} else {
				r.Aliases = []string{"Example ½"}
			}
			b, _ := json.Marshal(r)
			if _, e := f.store.db.Exec("UPDATE naming_rules SET body=? WHERE id=?", string(b), r.ID); e != nil {
				t.Fatal(e)
			}
			addRelease(t, f, "fixture", "one", "Example S09E02")
			f.tick(1000)
			if f.job().BlockReason != "rule_normalization_unresolved" {
				t.Fatal("unsafe fallback", f.job().BlockReason)
			}
		})
	}
}
func TestNewAliasStoredRawAndVersioned(t *testing.T) {
	f := newFixture(t, "Example.S09E02.mkv", -1)
	r := reviewRule()
	r.Aliases = []string{"Example (2026)"}
	installRule(t, f.store, r)
	got, e := f.store.Rule(context.Background(), r.ID, 1)
	if e != nil || got.IdentityNormalizationVersion != 1 || got.Aliases[0] != r.Aliases[0] {
		t.Fatal(got, e)
	}
	addRelease(t, f, "fixture", "one", "Example【2026】 [02]")
	f.tick(1000)
	if f.job().Import != "imported" || !strings.HasPrefix(f.job().Plan.Files[0].Target, "Confirmed/") {
		t.Fatal("structural bracket spacing lost identity", f.job().BlockReason)
	}
	p, e := ParseTitle("Example (2026) [02]")
	if e != nil || !got.matches("fixture", p) {
		t.Fatal("asymmetric new alias", e)
	}
}

func TestLegacyMetadataAliasLayoutSeparators(t *testing.T) {
	r := reviewRule()
	r.Aliases = []string{"Example (2026)"}
	for _, sep := range []string{" ", "_", "-", ".", " _- "} {
		for _, brackets := range [][2]string{{"[", "]"}, {"(", ")"}, {"【", "】"}} {
			p, e := ParseTitle("Example" + sep + brackets[0] + "2026" + brackets[1] + " [02]")
			if e != nil || !r.matches("fixture", p) {
				t.Fatal("metadata layout mismatch", sep, brackets, e)
			}
		}
	}
	p, e := ParseTitle("Example (2026) Extended [02]")
	if e != nil || r.matches("fixture", p) || !r.knownIdentity("fixture", p) {
		t.Fatal("identity extension must remain ambiguous", e)
	}
}
