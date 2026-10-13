package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func batchFixture(t *testing.T) (*fixture, BatchOrganization) {
	t.Helper()
	f := newFixture(t, "Episode [67].mkv", 7)
	f.manifest = []string{f.name, "Episode [67].zh-Hans.ass", "Episode [68].mp4", "notes.txt"}
	for _, s := range f.manifest {
		if err := os.WriteFile(filepath.Join(f.downloads, s), []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.tick(1000)
	if f.job().Import != "needs_confirmation" || f.job().Plan != nil {
		t.Fatal("not awaiting human", f.job())
	}
	b := BatchOrganization{Directory: "Human library", DisplayName: "Human Example", Season: 4, Mode: "extracted", Offset: -66, Rows: []BatchRow{{f.manifest[0], 1}, {f.manifest[2], 2}}}
	return f, b
}
func batchPreview(t *testing.T, f *fixture, b BatchOrganization) *BatchPreview {
	t.Helper()
	v, err := f.engine.BatchPreview(context.Background(), f.job().ID, f.job().Version, b)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func batchConfirm(t *testing.T, f *fixture, b BatchOrganization, v *BatchPreview) {
	t.Helper()
	if err := f.engine.BatchConfirm(context.Background(), f.job().ID, v.JobVersion, v.Plan.Version, v.Digest, b); err != nil {
		t.Fatal(err)
	}
}
func comparisons(t *testing.T, f *fixture) []ComparisonView {
	t.Helper()
	v, next, err := f.store.ComparisonPage(context.Background(), f.job().ID, 0)
	if err != nil || next != 0 {
		t.Fatal(next, err)
	}
	return v
}
func TestBatchWholePlanIntentAndCompletion(t *testing.T) {
	ctx := context.Background()
	f, b := batchFixture(t)
	before := f.job()
	v := batchPreview(t, f, b)
	if len(v.Plan.Files) != 3 || len(v.Plan.Manifest) != 4 || f.job().Version != before.Version || f.job().Plan != nil || len(comparisons(t, f)) != 0 {
		t.Fatal("preview mutated or omitted", v)
	}
	entries, _ := os.ReadDir(f.library)
	if len(entries) != 0 {
		t.Fatal("preview wrote media")
	}
	batchConfirm(t, f, b, v)
	records := comparisons(t, f)
	if len(records) != 1 || records[0].Status != "confirmed_intent" || records[0].Intent.JobVersion != before.Version || records[0].Intent.Origin != "human" || len(records[0].Intent.Manifest) != 4 || records[0].Intent.Organization.Season != 4 {
		t.Fatal(records)
	}
	raw, _ := json.Marshal(records[0].Intent)
	batchConfirm(t, f, b, v) // 原job version重放：不制造第二记录。
	if len(comparisons(t, f)) != 1 {
		t.Fatal("duplicate confirmation")
	}
	if _, err := f.store.db.Exec("UPDATE comparison_records SET body='{}'"); err == nil {
		t.Fatal("record mutable")
	}
	if _, err := f.store.db.Exec("DELETE FROM comparison_records"); err == nil {
		t.Fatal("record deletable")
	}
	f.tick(1001)
	if comparisons(t, f)[0].Status != "awaiting_discovery" || f.job().Discovery == "confirmed" {
		t.Fatal("2xx claimed ready")
	}
	for _, p := range f.job().Plan.Files {
		src, e := os.Stat(filepath.Join(f.downloads, p.Source))
		if e != nil {
			t.Fatal(e)
		}
		dst, e := os.Stat(filepath.Join(f.library, p.Target))
		if e != nil || !os.SameFile(src, dst) {
			t.Fatal("source changed or not hardlink", e)
		}
	}
	// 修改未来规则不能移动已开始方案或重写记录。
	r := manualRule()
	r.Directory = "future"
	installRule(t, f.store, r)
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	f.tick(1002)
	if f.job().Discovery != "confirmed" || comparisons(t, f)[0].Status != "complete" || PlanDigest(f.job().Plan) != v.Digest {
		t.Fatal("not ready or changed plan")
	}
	got, _ := json.Marshal(comparisons(t, f)[0].Intent)
	if string(got) != string(raw) {
		t.Fatal("intent changed")
	}
	batchConfirm(t, f, b, v)
	f.reopen()
	batchConfirm(t, f, b, v)
	if len(comparisons(t, f)) != 1 {
		t.Fatal("reopen duplicated")
	}
	events, _ := f.store.Events(ctx, 0)
	done := 0
	for _, event := range events {
		if event.Kind == "discovered" {
			done++
		}
		encoded, _ := json.Marshal(event)
		if strings.Contains(string(encoded), "Episode") || strings.Contains(string(encoded), "Human") {
			t.Fatal("private names in events")
		}
	}
	if done != 1 {
		t.Fatal("duplicate ready", done)
	}
}
func TestBatchRejectsIncompleteUnsafeAndUnsupported(t *testing.T) {
	cases := []struct {
		name   string
		change func(*fixture, *BatchOrganization)
		want   string
	}{
		{"omitted", func(f *fixture, b *BatchOrganization) { b.Rows = b.Rows[:1] }, "collection_incomplete"},
		{"duplicate source", func(f *fixture, b *BatchOrganization) { b.Rows[1].Source = b.Rows[0].Source }, "invalid_file_manifest"},
		{"duplicate episode", func(f *fixture, b *BatchOrganization) { b.Mode = "manual"; b.Offset = 0; b.Rows[1].Episode = 1 }, "duplicate_episode"},
		{"escape", func(f *fixture, b *BatchOrganization) { b.Directory = "../outside" }, "invalid_batch"},
		{"display escape", func(f *fixture, b *BatchOrganization) { b.DisplayName = "other/title" }, "invalid_batch"},
		{"special", func(f *fixture, b *BatchOrganization) { f.manifest = append(f.manifest, "SP01.mkv") }, "unsupported_episode_kind"},
		{"multi", func(f *fixture, b *BatchOrganization) {
			f.manifest[0] = "Episode [67~68].mkv"
			f.manifest[1] = "Episode [67~68].zh-Hans.ass"
			b.Rows[0].Source = f.manifest[0]
		}, "collection_preview_only"},
		{"mixed season", func(f *fixture, b *BatchOrganization) {
			f.manifest[0] = "Episode S03E67.mkv"
			f.manifest[1] = "Episode S03E67.zh-Hans.ass"
			b.Rows[0].Source = f.manifest[0]
		}, "season_conflict"},
		{"subtitle", func(f *fixture, b *BatchOrganization) { f.manifest[1] = "unrelated.ass" }, "subtitle_mapping_ambiguous"},
		{"unsupported", func(f *fixture, b *BatchOrganization) { f.manifest = append(f.manifest, "unsupported.avi") }, "unsupported_video"},
		{"offset bound", func(f *fixture, b *BatchOrganization) { b.Offset = -999 }, "episode_out_of_bounds"},
		{"extraction forged", func(f *fixture, b *BatchOrganization) { b.Rows[0].Episode = 3 }, "invalid_batch"},
		{"order unconfirmed", func(f *fixture, b *BatchOrganization) { b.Mode = "sequential"; b.Offset = 0; b.Start = 1 }, "order_confirmation_required"},
		{"order forged", func(f *fixture, b *BatchOrganization) {
			b.Mode = "sequential"
			b.Offset = 0
			b.Start = 1
			b.OrderConfirmed = true
			b.Rows[0].Episode = 2
		}, "invalid_batch"},
		{"target conflict", func(f *fixture, b *BatchOrganization) {
			p := filepath.Join(f.library, "Human library/Season 04")
			os.MkdirAll(p, 0750)
			os.WriteFile(filepath.Join(p, "Human Example - S04E01.mkv"), []byte("foreign"), 0600)
		}, "target_conflict"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, b := batchFixture(t)
			tc.change(f, &b)
			j := f.job()
			j.Sources = f.manifest
			if err := f.store.Save(context.Background(), &j, "fixture_manifest"); err != nil {
				t.Fatal(err)
			}
			_, err := f.engine.BatchPreview(context.Background(), f.job().ID, j.Version, b)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v want %s", err, tc.want)
			}
			if len(comparisons(t, f)) != 0 || f.job().Plan != nil {
				t.Fatal("rejection mutated")
			}
		})
	}
}
func TestBatchManualAndExplicitOrder(t *testing.T) {
	for _, mode := range []string{"manual", "sequential"} {
		t.Run(mode, func(t *testing.T) {
			f, b := batchFixture(t)
			b.Mode = mode
			b.Offset = 0
			if mode == "manual" {
				b.Rows[0].Episode = 9
				b.Rows[1].Episode = 12
			} else {
				b.Start = 5
				b.OrderConfirmed = true
				b.Rows = []BatchRow{{f.manifest[2], 5}, {f.manifest[0], 6}}
			}
			v := batchPreview(t, f, b)
			batchConfirm(t, f, b, v)
			f.tick(1001)
			if f.job().Import != "imported" || f.job().Plan.Batch.Rows[0] != b.Rows[0] {
				t.Fatal("human order changed")
			}
		})
	}
}
func TestBatchStalenessFullFingerprintsAndReservations(t *testing.T) {
	for _, what := range []string{"job", "auxiliary", "video", "subtitle", "rules", "target", "other-job"} {
		t.Run(what, func(t *testing.T) {
			f, b := batchFixture(t)
			v := batchPreview(t, f, b)
			ctx := context.Background()
			switch what {
			case "job":
				j := f.job()
				if err := f.store.Save(ctx, &j, "fixture_update"); err != nil {
					t.Fatal(err)
				}
			case "rules":
				installRule(t, f.store, manualRule())
			case "auxiliary", "video", "subtitle":
				s := map[string]string{"auxiliary": "notes.txt", "video": f.manifest[0], "subtitle": f.manifest[1]}[what]
				if err := os.WriteFile(filepath.Join(f.downloads, s), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "target":
				p := filepath.Join(f.library, filepath.Dir(v.Plan.Files[0].Target))
				os.MkdirAll(p, 0750)
				os.WriteFile(filepath.Join(f.library, v.Plan.Files[0].Target), []byte("foreign"), 0600)
			case "other-job":
				if _, err := f.store.db.Exec("INSERT INTO target_reservations VALUES(?,?,?)", v.Plan.Files[0].Target, f.job().ID, 123); err != nil {
					t.Fatal(err)
				}
			}
			err := f.engine.BatchConfirm(ctx, f.job().ID, v.JobVersion, v.Plan.Version, v.Digest, b)
			if err == nil {
				t.Fatal("stale/conflict approved", what)
			}
			if len(comparisons(t, f)) != 0 || f.job().Plan != nil {
				t.Fatal("failed confirmation wrote intent")
			}
		})
	}
}
func TestBatchConfirmationTransactionRollback(t *testing.T) {
	for _, at := range []string{"comparison_records", "events", "plan_history", "file_execution"} {
		t.Run(at, func(t *testing.T) {
			f, b := batchFixture(t)
			v := batchPreview(t, f, b)
			if _, err := f.store.db.Exec("CREATE TRIGGER fail_confirmation BEFORE INSERT ON " + at + " BEGIN SELECT RAISE(ABORT,'fixture failure'); END;"); err != nil {
				t.Fatal(err)
			}
			if err := f.engine.BatchConfirm(context.Background(), f.job().ID, v.JobVersion, v.Plan.Version, v.Digest, b); err == nil {
				t.Fatal("fault ignored")
			}
			if f.job().Plan != nil || len(comparisons(t, f)) != 0 {
				t.Fatal("partial transaction")
			}
			var reservations int
			if err := f.store.db.QueryRow("SELECT count(*) FROM target_reservations").Scan(&reservations); err != nil || reservations != 0 {
				t.Fatal(reservations, err)
			}
			if _, err := f.store.db.Exec("DROP TRIGGER fail_confirmation"); err != nil {
				t.Fatal(err)
			}
			batchConfirm(t, f, b, v)
			if len(comparisons(t, f)) != 1 {
				t.Fatal("retry failed")
			}
		})
	}
}

type batchCrashFiles struct {
	FileOperations
	calls int
}

func (f *batchCrashFiles) LinkContext(ctx context.Context, p PlannedFile) error {
	f.calls++
	if err := f.FileOperations.LinkContext(ctx, p); err != nil {
		return err
	}
	if f.calls == 2 {
		return errors.New("link_failed")
	}
	return nil
}
func TestBatchPartialAndLinkCrashRecoverySeedIndependent(t *testing.T) {
	f, b := batchFixture(t)
	v := batchPreview(t, f, b)
	batchConfirm(t, f, b, v)
	f.engine.Files = &batchCrashFiles{FileOperations: f.files}
	if err := f.engine.Tick(context.Background(), time.Unix(1001, 0)); err == nil {
		t.Fatal("missing crash")
	}
	j := f.job()
	if !j.Plan.Started || j.Import == "imported" || comparisons(t, f)[0].Status != "partial" {
		t.Fatal("false success")
	}
	records := comparisons(t, f)
	if records[0].Files[0].State != "linked" || records[0].Files[1].State != "intended" {
		t.Fatal("wrong recovery facts", records)
	}
	if _, err := f.engine.BatchPreview(context.Background(), f.job().ID, j.Version, b); err == nil || err.Error() != "plan_frozen" {
		t.Fatal("frozen edited", err)
	}
	// 修改辅助指纹令全包重试失败，但到期stop仍先执行。
	os.WriteFile(filepath.Join(f.downloads, "notes.txt"), []byte("changed"), 0600)
	if err := f.engine.Tick(context.Background(), time.Unix(j.StopAt, 0)); err == nil {
		t.Fatal("missing preflight error")
	}
	if f.job().Seed != "stopped" || comparisons(t, f)[0].Status != "partial" {
		t.Fatal("seed blocked or false ready")
	}
	// 另一个 fixture 用未变化源重开，验证链接成功但状态落盘前崩溃的重放。
	g, c := batchFixture(t)
	w := batchPreview(t, g, c)
	batchConfirm(t, g, c, w)
	g.engine.Files = &batchCrashFiles{FileOperations: g.files}
	if err := g.engine.Tick(context.Background(), time.Unix(1001, 0)); err == nil {
		t.Fatal("missing crash")
	}
	g.reopen()
	g.tick(1002)
	g.mu.Lock()
	g.discovered = true
	g.mu.Unlock()
	g.tick(1003)
	if comparisons(t, g)[0].Status != "complete" || len(comparisons(t, g)) != 1 || PlanDigest(g.job().Plan) != w.Digest {
		t.Fatal("not recovered")
	}
}
func TestBatchSchemaMigrationAndRollback(t *testing.T) {
	f, b := batchFixture(t)
	v := batchPreview(t, f, b)
	batchConfirm(t, f, b, v)
	f.reopen()
	var version int
	if err := f.store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 6 {
		t.Fatal(version, err)
	}
	if len(comparisons(t, f)) != 1 {
		t.Fatal("migration lost record")
	}
	p := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA user_version=5; CREATE VIEW comparison_records AS SELECT 1;"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := OpenStore(p); err == nil {
		s.Close()
		t.Fatal("migration collision ignored")
	}
	db, err = sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatal("migration not rolled back", version, err)
	}
}
