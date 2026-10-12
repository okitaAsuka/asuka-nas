package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIndependentV2SpecialEpisodeBypasses(t *testing.T) {
	for _, tc := range []struct {
		name, title, source string
		bounded             bool
	}{
		{"unbounded_source_sp", "Example [02]", "Example SP [02].mkv", false},
		{"unbounded_rss_sp", "Example [02] [SP]", "Example [02].mkv", false},
		{"bounded_rss_sp", "Example [02] [SP]", "Example [02].mkv", true},
		{"unbounded_source_ova", "Example [02]", "Example OVA [02].mkv", false},
		{"bounded_source_control", "Example [02]", "Example SP [02].mkv", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.source, -1)
			r := reviewRule()
			if tc.bounded {
				r.Episodes = &EpisodeRange{1, 11}
			}
			installRule(t, f.store, r)
			addRelease(t, f, "fixture", "one", tc.title)
			f.tick(1000)
			j := f.job()
			if j.Import == "imported" {
				t.Fatalf("special imported as regular: %s", j.Plan.Files[0].Target)
			}
		})
	}
}

func TestIndependentV2DisabledRulePunctuation(t *testing.T) {
	for _, title := range []string{"Example: S09E02", "Example：S09E02", "Example【02】"} {
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
			if j.Import == "imported" {
				t.Fatalf("disabled known identity bypassed: %s", j.Plan.Files[0].Target)
			}
		})
	}
}

func TestIndependentV2FractionAndRangeSuffix(t *testing.T) {
	t.Run("nfkc_fraction", func(t *testing.T) {
		f := newFixture(t, "Example S09E02½.mkv", -1)
		installRule(t, f.store, reviewRule())
		addRelease(t, f, "fixture", "one", "Example [21]")
		f.tick(1000)
		j := f.job()
		if j.Import == "imported" {
			t.Fatalf("fraction became E21: %s", j.Plan.Files[0].Target)
		}
	})
	t.Run("range_decimal_endpoint", func(t *testing.T) {
		f := newFixture(t, "Example [01].mkv", -1)
		name := "Example [02].mkv"
		if err := os.WriteFile(filepath.Join(f.downloads, name), []byte("two"), 0600); err != nil {
			t.Fatal(err)
		}
		f.manifest = []string{f.name, name}
		r := reviewRule()
		r.Episodes = &EpisodeRange{1, 2}
		installRule(t, f.store, r)
		addRelease(t, f, "fixture", "one", "Example - 01-02.5")
		f.tick(1000)
		j := f.job()
		if j.Import == "imported" {
			t.Fatalf("unsupported RSS range endpoint silently truncated: decisions=%v", j.Plan.Decisions)
		}
	})
}

func reviewDowngradeV3(t *testing.T, f *fixture) {
	t.Helper()
	if _, err := f.store.db.Exec("DROP TABLE file_execution; DROP TABLE target_reservations; ALTER TABLE events DROP COLUMN plan_version; ALTER TABLE events DROP COLUMN file_index; PRAGMA user_version=3"); err != nil {
		t.Fatal(err)
	}
}
func TestIndependentV2MigrationPrefixConflict(t *testing.T) {
	f := newFixture(t, "Example.S09E02.mkv", 0)
	ctx := context.Background()
	j := f.job()
	j.Download = "completed"
	j.Sources = []string{f.name}
	j.CompletedAt = 1000
	j.StopAt = 1000
	j.Seed = "stopped"
	var err error
	j.Plan, err = f.files.ProposedManifest(ctx, f.name, "Parent.mkv", j.Version+1, []string{f.name})
	if err != nil {
		t.Fatal(err)
	}
	j.Plan.Approved = true
	j.Import = "planned"
	other := j
	other.ID = "other-job"
	other.Hash = strings.Repeat("a", 40)
	other.Plan, err = f.files.ProposedManifest(ctx, f.name, "Parent.mkv/Child.mkv", other.Version+1, []string{f.name})
	if err != nil {
		t.Fatal(err)
	}
	other.Plan.Approved = true
	for _, v := range []Job{j, other} {
		b, _ := json.Marshal(v)
		if _, err := f.store.db.Exec("INSERT OR REPLACE INTO jobs VALUES(?,?,?,?)", v.ID, v.Hash, v.Version, string(b)); err != nil {
			t.Fatal(err)
		}
	}
	reviewDowngradeV3(t, f)
	f.store.Close()
	migrated, err := OpenStore(f.db)
	if err == nil {
		defer migrated.Close()
		var n int
		if e := migrated.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM target_reservations").Scan(&n); e != nil {
			t.Fatal(e)
		}
		t.Fatalf("migration accepted mutually exclusive file/parent reservations: count=%d", n)
	}
	raw, e := sql.Open("sqlite", f.db)
	if e != nil {
		t.Fatal(e)
	}
	defer raw.Close()
	var version int
	if e = raw.QueryRow("PRAGMA user_version").Scan(&version); e != nil || version != 3 {
		t.Fatal(version, e)
	}
}

func TestIndependentV2MigratedManualDuplicateEvidence(t *testing.T) {
	f := newFixture(t, "Example.S09E02.mkv", -1)
	ctx := context.Background()
	addRelease(t, f, "fixture", "one", "Example S09E02")
	f.tick(1000)
	j := f.job()
	// v3 strict plans have no Manual/Evidence fields, as in the original commit.
	j.Plan.Manual = nil
	j.Plan.Evidence = nil
	j.Plan.ParserVersion = 1
	b, _ := json.Marshal(j)
	if _, err := f.store.db.Exec("UPDATE jobs SET body=? WHERE id=?", string(b), j.ID); err != nil {
		t.Fatal(err)
	}
	reviewDowngradeV3(t, f)
	f.reopen()
	addRelease(t, f, "fixture", "same-title-new-guid", "Example S09E02")
	if err := f.engine.RetryTick(ctx, time.Unix(1001, 0), 1); err != nil {
		t.Fatal(err)
	}
	if got := f.job(); got.BlockReason != "" {
		t.Fatalf("unchanged legacy evidence blocked after upgrade: %s", got.BlockReason)
	}
}

func TestIndependentV2PackIdentityControl(t *testing.T) {
	// 独立 3 集 fixture 用于完整清单/缺集/重复/不同季的对照。
	for _, mode := range []string{"complete", "missing", "duplicate", "season_conflict"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, "Example [01].mkv", -1)
			f.manifest = []string{f.name}
			r := reviewRule()
			r.Episodes = &EpisodeRange{1, 3}
			installRule(t, f.store, r)
			for i := 2; i <= 3; i++ {
				if mode == "missing" && i == 3 {
					continue
				}
				name := fmt.Sprintf("Example [%02d].mkv", i)
				if mode == "duplicate" && i == 3 {
					name = "Duplicate [02].mkv"
				}
				if mode == "season_conflict" && i == 3 {
					name = "Example S08E03.mkv"
				}
				if err := os.WriteFile(filepath.Join(f.downloads, name), []byte(name), 0600); err != nil {
					t.Fatal(err)
				}
				f.manifest = append(f.manifest, name)
			}
			addRelease(t, f, "fixture", "one", "Example - 01-03")
			f.tick(1000)
			j := f.job()
			if mode == "complete" {
				if j.Import != "imported" || len(j.Plan.Decisions) != 3 {
					t.Fatal(j)
				}
			} else {
				if j.Import != "needs_confirmation" {
					t.Fatal(j)
				}
				entries, _ := os.ReadDir(f.library)
				if len(entries) != 0 {
					t.Fatal("partial files")
				}
			}
		})
	}
}
