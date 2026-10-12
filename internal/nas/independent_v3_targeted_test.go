package nas

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIndependentV3SpecialDelimiterVariants(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		blocked      bool
	}{
		{"underscore_sp", "Example_SP_S09E02.mkv", true},
		{"underscore_ova", "Example_OVA_S09E02.mkv", true},
		{"dot_sp_control", "Example.SP.S09E02.mkv", true},
		{"bracket_sp_control", "Example [SP] S09E02.mkv", true},
		{"ordinary_control", "Example.S09E02.mkv", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.source, -1)
			installRule(t, f.store, reviewRule())
			addRelease(t, f, "fixture", "one", "Example [02]")
			f.tick(1000)
			j := f.job()
			if tc.blocked {
				if j.Import == "imported" {
					t.Fatalf("special delimiter bypass imported %s as %s", tc.source, j.Plan.Files[0].Target)
				}
			} else if j.Import != "imported" {
				t.Fatalf("ordinary regressed: %s", j.BlockReason)
			}
		})
	}
}

func TestIndependentV3NumericAndAliasNeighbors(t *testing.T) {
	for _, s := range []string{"Example - 01~02,5", "Example S09E02&#x00bd;", "Example [01-02.5]", "Example [总第67~68.5]", "Example - 01-002v2"} {
		if _, err := ParseTitle(s); err == nil {
			t.Errorf("unsupported token accepted: %s", s)
		}
	}
	for _, title := range []string{"Example：［０２］", "Example〔02〕", "Example（02）", "Example: S09E02"} {
		t.Run(title, func(t *testing.T) {
			f := newFixture(t, "Example.S09E02.mkv", -1)
			r := reviewRule()
			installRule(t, f.store, r)
			if err := f.store.SetRule(context.Background(), r.ID, 1, false); err != nil {
				t.Fatal(err)
			}
			addRelease(t, f, "fixture", "one", title)
			f.tick(1000)
			if j := f.job(); j.Import != "needs_confirmation" {
				t.Fatalf("disabled alias escaped: %s", title)
			}
		})
	}
}

func TestIndependentV3LegacyBridgeRollbackAndLateConflict(t *testing.T) {
	f := newFixture(t, "Example.S09E02.mkv", -1)
	ctx := context.Background()
	addRelease(t, f, "fixture", "one", "Example S09E02")
	f.tick(1000)
	j := f.job()
	j.Plan.Manual = nil
	j.Plan.Evidence = nil
	j.Plan.ParserVersion = 1
	digest := PlanDigest(j.Plan)
	b, _ := json.Marshal(j)
	if _, err := f.store.db.Exec("UPDATE jobs SET body=? WHERE id=?", string(b), j.ID); err != nil {
		t.Fatal(err)
	}
	// Simulate a genuine v4 database without the v5 evidence table.
	if _, err := f.store.db.Exec("DROP TABLE legacy_plan_evidence; PRAGMA user_version=4"); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	j = f.job()
	if j.BlockReason != "" || PlanDigest(j.Plan) != digest {
		t.Fatal("v4 bridge changed frozen plan or blocked proof", j.BlockReason)
	}
	addRelease(t, f, "fixture", "duplicate", "Example S09E02")
	f.tick(1001)
	if f.job().BlockReason != "" {
		t.Fatal("duplicate rejected")
	}
	addRelease(t, f, "fixture", "conflict", "Example S08E03")
	f.reopen()
	f.tick(1002)
	j = f.job()
	if j.BlockReason != "late_mapping_conflict" || PlanDigest(j.Plan) != digest {
		t.Fatal("new conflict ignored or plan rewritten")
	}
	if _, err := os.Stat(filepath.Join(f.library, j.Plan.Files[0].Target)); err != nil {
		t.Fatal("historical file moved", err)
	}
	var version int
	if err := f.store.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatal(version, err)
	}
}

func TestIndependentV3PersistedParenthesizedAlias(t *testing.T) {
	f := newFixture(t, "Example.S09E02.mkv", -1)
	ctx := context.Background()
	r := reviewRule()
	installRule(t, f.store, r)
	if err := f.store.SetRule(ctx, r.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	// 旧版 NFKC 规范化接受的合法 alias，模拟现有 v4 不可变规则行。
	r.Aliases = []string{"Example (2026)"}
	b, _ := json.Marshal(r)
	if _, err := f.store.db.Exec("UPDATE naming_rules SET body=? WHERE id=? AND revision=1", string(b), r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.db.Exec("DROP TABLE legacy_plan_evidence; PRAGMA user_version=4"); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	addRelease(t, f, "fixture", "one", "Example (2026) [02]")
	f.tick(1000)
	if j := f.job(); j.Import == "imported" {
		t.Fatalf("persisted disabled alias lost by new normalization: %s", j.Plan.Files[0].Target)
	}
}
