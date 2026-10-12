package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 仅合成 fixture：第四季及 67→1 为测试人员明确确认的映射，非现实作品元数据。
const packTitle = "&#91;测试组&#93;&#91;虚构作品第四季 / Fiction 4th - 01～11&#93;&#91;总第67～77&#93;[1080p][HEVC][简繁内封]"

func packRule() NamingRule {
	r := manualRule()
	r.Aliases = []string{"虚构作品第四季", "Fiction 4th"}
	r.Season = 4
	r.Episodes = &EpisodeRange{1, 11}
	r.Absolute = &AbsoluteMapping{67, 77, 1}
	return r
}
func packFixture(t *testing.T, mode string) *fixture {
	t.Helper()
	f := newFixture(t, "fixture.txt", -1)
	f.manifest = []string{"fixture.txt"}
	for i := 11; i >= 1; i-- {
		name := fmt.Sprintf("虚构作品 [%02d].mkv", i)
		if mode == "absolute" || mode == "mixed" && i%2 == 0 {
			name = fmt.Sprintf("虚构作品 [总第%d].mkv", 66+i)
		}
		if mode == "bare_absolute" {
			name = fmt.Sprintf("虚构作品 [%02d].mkv", 66+i)
		}
		if e := os.WriteFile(filepath.Join(f.downloads, name), []byte(fmt.Sprintf("episode %d", i)), 0600); e != nil {
			t.Fatal(e)
		}
		f.manifest = append(f.manifest, name)
		sub := strings.TrimSuffix(name, ".mkv") + ".zh-Hans.ass"
		if e := os.WriteFile(filepath.Join(f.downloads, sub), []byte(fmt.Sprintf("subtitle %d", i)), 0600); e != nil {
			t.Fatal(e)
		}
		f.manifest = append(f.manifest, sub)
	}
	installRule(t, f.store, packRule())
	addRelease(t, f, "fixture", "pack", packTitle)
	return f
}
func TestFullEncodedTitlesAndScopes(t *testing.T) {
	title := "&#91;合成字幕组&#93;[东京复仇者 三天战争篇 / Tokyo Revengers:Santen Sensou][02][1080P][HEVC_AAC][简繁内封]"
	p, e := ParseTitle(title)
	if e != nil || p.Season != 0 || p.Relative == nil || p.Relative.Start != 2 || p.Group != "合成字幕组" {
		t.Fatal(p, e)
	}
	// 完整括号后紧接 [02] 也应命中别名，不能只支持带空格的简化示例。
	r := manualRule()
	r.Group = "合成字幕组"
	r.Aliases = []string{"东京复仇者 三天战争篇", "Tokyo Revengers:Santen Sensou"}
	if !r.matches("fixture", p) {
		t.Fatal("full title alias lost")
	}
	p, e = ParseTitle(packTitle)
	if e != nil || p.Season != 4 || p.Relative == nil || *p.Relative != (EpisodeRange{1, 11}) || p.Absolute == nil || *p.Absolute != (EpisodeRange{67, 77}) {
		t.Fatal(p, e)
	}
	f := newFixture(t, "虚构作品 [02].mkv", -1)
	broad := manualRule()
	broad.Group = ""
	broad.ID = "broad"
	broad.Directory = "broad-target"
	installRule(t, f.store, broad)
	installRule(t, f.store, manualRule())
	addRelease(t, f, "fixture", "one", "[测试组] 虚构作品 篇章 [02]")
	f.tick(1000)
	if f.job().BlockReason != "mapping_conflict" {
		t.Fatal("specific scope silently won")
	}
	if manualRule().matches("other", p) {
		t.Fatal("subscription scope escaped")
	}
}
func TestCollectionElevenVideosRelativeAbsoluteMixed(t *testing.T) {
	for _, mode := range []string{"relative", "absolute", "mixed", "bare_absolute"} {
		t.Run(mode, func(t *testing.T) {
			f := packFixture(t, mode)
			f.tick(1000)
			j := f.job()
			if j.Import != "imported" || j.Discovery != "pending" || !j.Plan.Collection || len(j.Plan.Decisions) != 11 || len(j.Plan.Files) != 22 || len(j.Plan.Manifest) != 23 {
				t.Fatalf("invalid pack: %s %s %s", j.Import, j.BlockReason, j.ErrorCode)
			}
			eps := map[int]bool{}
			for _, d := range j.Plan.Decisions {
				eps[d.Episode] = true
				if d.Season != 4 || d.SeriesID != packRule().SeriesID {
					t.Fatal("identity changed")
				}
			}
			for i := 1; i <= 11; i++ {
				if !eps[i] {
					t.Fatal("episode omitted", i)
				}
			}
			for _, p := range j.Plan.Files {
				a, _ := os.Stat(filepath.Join(f.downloads, p.Source))
				b, e := os.Stat(filepath.Join(f.library, p.Target))
				if e != nil || !os.SameFile(a, b) {
					t.Fatal("source/target not same inode", p.Source, e)
				}
			}
			f.mu.Lock()
			f.discovered = true
			f.mu.Unlock()
			f.tick(1001)
			if f.job().Discovery != "confirmed" {
				t.Fatal("not ready")
			}
			before, _ := f.store.Events(context.Background(), 0)
			f.tick(1002)
			after, _ := f.store.Events(context.Background(), 0)
			if len(before) != len(after) {
				t.Fatal("duplicate completed events")
			}
		})
	}
}
func TestCollectionAmbiguitiesBlockWholePack(t *testing.T) {
	cases := []struct {
		name   string
		change func(*fixture)
		reason string
	}{
		{"missing", func(f *fixture) { f.manifest = f.manifest[:len(f.manifest)-2] }, "collection_incomplete"},
		{"duplicate", func(f *fixture) {
			f.manifest = append(f.manifest, "Other [01].mkv")
			os.WriteFile(filepath.Join(f.downloads, "Other [01].mkv"), []byte("duplicate"), 0600)
		}, "duplicate_episode"},
		{"numberless", func(f *fixture) {
			f.manifest = append(f.manifest, "numberless.mkv")
			os.WriteFile(filepath.Join(f.downloads, "numberless.mkv"), []byte("unknown"), 0600)
		}, "episode_unresolved"},
		{"special", func(f *fixture) {
			f.manifest = append(f.manifest, "Show SP01 [01].mkv")
			os.WriteFile(filepath.Join(f.downloads, "Show SP01 [01].mkv"), []byte("special"), 0600)
		}, "unsupported_episode_kind"},
		{"combined", func(f *fixture) {
			f.manifest = []string{"Show.S04E01E02.mkv"}
			os.WriteFile(filepath.Join(f.downloads, f.manifest[0]), []byte("combined"), 0600)
		}, "collection_preview_only"},
		{"single_of_eleven", func(f *fixture) {
			f.manifest = []string{"Show [01].mkv"}
			os.WriteFile(filepath.Join(f.downloads, f.manifest[0]), []byte("one"), 0600)
		}, "collection_incomplete"},
		{"absolute_conflict", func(f *fixture) {
			f.manifest = append(f.manifest, "Show [02] [总第67].mkv")
			os.WriteFile(filepath.Join(f.downloads, "Show [02] [总第67].mkv"), []byte("conflict"), 0600)
		}, "absolute_relative_conflict"},
		{"outside", func(f *fixture) {
			f.manifest = append(f.manifest, "Show [总第78].mkv")
			os.WriteFile(filepath.Join(f.downloads, "Show [总第78].mkv"), []byte("outside"), 0600)
		}, "absolute_mapping_unresolved"},
		{"subtitle", func(f *fixture) {
			f.manifest = append(f.manifest, "unowned.ass")
			os.WriteFile(filepath.Join(f.downloads, "unowned.ass"), []byte("subtitle"), 0600)
		}, "subtitle_mapping_ambiguous"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := packFixture(t, "relative")
			tc.change(f)
			f.tick(1000)
			j := f.job()
			if j.Import != "needs_confirmation" || j.BlockReason != tc.reason || j.Seed != "stopped" {
				t.Fatalf("got %s %s %s", j.Import, j.BlockReason, j.ErrorCode)
			}
			entries, _ := os.ReadDir(f.library)
			if len(entries) != 0 {
				t.Fatal("partial pack escaped")
			}
		})
	}
	f := packFixture(t, "relative")
	if e := f.store.AttachResourceTitle(context.Background(), "fixture", "pack", strings.Replace(packTitle, "01～11", "01～10", 1)); e != nil {
		t.Fatal(e)
	}
	f.tick(1000)
	if f.job().BlockReason != "absolute_relative_conflict" {
		t.Fatal("title range contradiction lost")
	}
}

type faultFiles struct {
	FileOperations
	at, calls  int
	after      func() error
	permission bool
}

func (f *faultFiles) LinkContext(ctx context.Context, p PlannedFile) error {
	f.calls++
	if f.calls == f.at && f.permission {
		return errors.New("permission_denied")
	}
	if e := f.FileOperations.LinkContext(ctx, p); e != nil {
		return e
	}
	if f.calls == f.at && f.after != nil {
		return f.after()
	}
	return nil
}
func TestCollectionMidLinkDatabaseFailureAndFrozenRecovery(t *testing.T) {
	f := packFixture(t, "mixed")
	ctx := context.Background()
	f.engine.Files = &faultFiles{FileOperations: f.files, at: 5, after: func() error {
		_, e := f.store.db.Exec(`CREATE TRIGGER reject_fifth BEFORE INSERT ON events WHEN NEW.kind='file_linked' AND NEW.file_index=4 BEGIN SELECT RAISE(ABORT,'fixture'); END`)
		return e
	}}
	if e := f.engine.Tick(ctx, time.Unix(1000, 0)); e == nil {
		t.Fatal("expected DB failure")
	}
	j := f.job()
	if j.Import != "planned" || !j.Plan.Started || j.Seed != "stopped" {
		t.Fatal("false imported")
	}
	records, e := f.store.FileExecutions(ctx, j.ID, j.Plan.Version)
	if e != nil || records[4].State != "intended" || records[3].State != "linked" || records[5].State != "pending" {
		t.Fatal(records, e)
	}
	target := filepath.Join(f.library, j.Plan.Files[4].Target)
	before, e := os.Stat(target)
	if e != nil {
		t.Fatal("fifth link not created")
	}
	digest := PlanDigest(j.Plan)
	if _, e = f.store.db.Exec("DROP TRIGGER reject_fifth"); e != nil {
		t.Fatal(e)
	}
	r := packRule()
	r.Revision = 2
	r.Directory = "new-rule-target"
	installRule(t, f.store, r)
	f.reopen()
	f.tick(1001)
	j = f.job()
	after, e := os.Stat(target)
	if e != nil || !os.SameFile(before, after) || j.Import != "imported" || PlanDigest(j.Plan) != digest {
		t.Fatal("frozen recovery failed", e)
	}
	events, _ := f.store.Events(ctx, 0)
	links := map[int]int{}
	imports := 0
	for _, v := range events {
		if v.Kind == "file_linked" {
			if v.FileIndex == nil || v.PlanVersion == nil {
				t.Fatal("missing event correlation")
			}
			links[*v.FileIndex]++
		}
		if v.Kind == "imported" {
			imports++
		}
	}
	if len(links) != 22 || imports != 1 {
		t.Fatal(links, imports)
	}
	for _, n := range links {
		if n != 1 {
			t.Fatal("duplicate link event")
		}
	}
}
func TestCollectionPermissionFailureRecovery(t *testing.T) {
	f := packFixture(t, "relative")
	f.engine.Files = &faultFiles{FileOperations: f.files, at: 7, permission: true}
	if e := f.engine.Tick(context.Background(), time.Unix(1000, 0)); e == nil {
		t.Fatal("expected permission failure")
	}
	j := f.job()
	if j.Import != "planned" || j.ErrorCode != "permission_denied" || j.Seed != "stopped" {
		t.Fatal("bad failure state")
	}
	if _, e := os.Stat(filepath.Join(f.library, j.Plan.Files[5].Target)); e != nil {
		t.Fatal("prior links deleted")
	}
	f.reopen()
	f.tick(1001)
	if f.job().Import != "imported" {
		t.Fatal("permission recovery failed")
	}
}

type partialMedia struct {
	Media
	visible map[string]bool
	calls   map[string]int
}

func (m *partialMedia) Discover(_ context.Context, path string) (bool, error) {
	m.calls[path]++
	return m.visible[path], nil
}
func TestAllVideoDiscoveryRequiredAndPersists(t *testing.T) {
	f := packFixture(t, "absolute")
	media := &partialMedia{Media: f.engine.Emby, visible: map[string]bool{}, calls: map[string]int{}}
	f.engine.Emby = media
	f.tick(1000)
	j := f.job()
	for i, p := range j.Plan.Files {
		if videoTarget(p.Target) && i < 12 {
			media.visible[filepath.Join(f.library, p.Target)] = true
		}
	}
	f.tick(1001)
	if f.job().Discovery == "confirmed" {
		t.Fatal("partial discovery became ready")
	}
	f.reopen()
	media.Media = f.engine.Emby
	f.engine.Emby = media
	prior := map[string]int{}
	for p, n := range media.calls {
		prior[p] = n
	}
	for _, p := range j.Plan.Files {
		if videoTarget(p.Target) {
			media.visible[filepath.Join(f.library, p.Target)] = true
		}
	}
	f.tick(1002)
	if f.job().Discovery != "confirmed" {
		t.Fatal("all videos not ready")
	}
	for i, p := range j.Plan.Files {
		full := filepath.Join(f.library, p.Target)
		if videoTarget(p.Target) && i < 12 && media.calls[full] != prior[full] {
			t.Fatal("persisted discovery queried again")
		}
	}
	f.tick(1003)
	events, _ := f.store.Events(context.Background(), 0)
	ready := 0
	discovered := 0
	for _, v := range events {
		if v.Kind == "discovered" {
			ready++
		}
		if v.Kind == "video_discovered" {
			discovered++
		}
	}
	if ready != 1 || discovered != 11 {
		t.Fatal("wrong notifications", ready, discovered)
	}
}
func TestCrossJobReservationAndAtomicRollback(t *testing.T) {
	f := packFixture(t, "relative")
	ctx := context.Background()
	j := f.job()
	j.Sources = f.manifest
	p, e := f.engine.collectionPlan(ctx, &j)
	if e != nil {
		t.Fatal(e)
	}
	j.Plan = p
	j.Import = "planned"
	if e = f.store.Save(ctx, &j, "plan_ready"); e != nil {
		t.Fatal(e)
	}
	id, e := f.store.IngestRelease(ctx, "fixture", "second", strings.Repeat("b", 40), "magnet", nil, 0, packTitle)
	if e != nil {
		t.Fatal(e)
	}
	other, e := f.store.Get(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	other.Sources = f.manifest
	p2, e := f.engine.collectionPlan(ctx, &other)
	if e != nil {
		t.Fatal(e)
	}
	p2.Files[0].Target = "Unreserved-first.mkv" // 后续目标冲突必须回滚前面已插入的预约。
	other.Plan = p2
	other.Import = "planned"
	if e = f.store.Save(ctx, &other, "plan_ready"); e == nil || e.Error() != "target_reserved" {
		t.Fatal("cross-task target accepted", e)
	}
	var count int
	if e = f.store.db.QueryRow("SELECT COUNT(*) FROM target_reservations WHERE job_id=?", id).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial reservations", count, e)
	}
	if e = f.store.db.QueryRow("SELECT COUNT(*) FROM plan_history WHERE job_id=?", id).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial history", count, e)
	}
}
func TestSchemaV3PlanMigrationAndConflictRollback(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			f := newFixture(t, "Show.S01E01.mkv", 0)
			f.tick(1000)
			j := f.job()
			if conflict {
				other := j
				other.ID = "legacy-other"
				other.Hash = "legacy-other"
				b, _ := json.Marshal(other)
				if _, e := f.store.db.Exec("INSERT INTO jobs VALUES(?,?,?,?)", other.ID, other.Hash, other.Version, string(b)); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := f.store.db.Exec("DROP TABLE file_execution; DROP TABLE target_reservations; ALTER TABLE events DROP COLUMN plan_version; ALTER TABLE events DROP COLUMN file_index; PRAGMA user_version=3;"); e != nil {
				t.Fatal(e)
			}
			f.store.Close()
			s, e := OpenStore(f.db)
			if conflict {
				if e == nil {
					s.Close()
					t.Fatal("legacy duplicate target accepted")
				}
				raw, err := sql.Open("sqlite", f.db)
				if err != nil {
					t.Fatal(err)
				}
				defer raw.Close()
				var version, count int
				if err = raw.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
					t.Fatal("migration version not rolled back", version, err)
				}
				if err = raw.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='target_reservations'").Scan(&count); err != nil || count != 0 {
					t.Fatal("migration DDL not rolled back", count, err)
				}
				if err = raw.QueryRow("SELECT COUNT(*) FROM jobs").Scan(&count); err != nil || count != 2 {
					t.Fatal("legacy data changed", count, err)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			f.store = s
			f.engine.Store = s
			records, e := s.FileExecutions(context.Background(), j.ID, j.Plan.Version)
			if e != nil || len(records) != 1 || records[0].State != "linked" {
				t.Fatal(records, e)
			}
			f.tick(1001)
		})
	}
}

func TestFileIntentMustCommitBeforeLink(t *testing.T) {
	f := packFixture(t, "relative")
	if _, e := f.store.db.Exec(`CREATE TRIGGER reject_intent BEFORE INSERT ON events WHEN NEW.kind='file_intent' AND NEW.file_index=5 BEGIN SELECT RAISE(ABORT,'fixture'); END`); e != nil {
		t.Fatal(e)
	}
	if e := f.engine.Tick(context.Background(), time.Unix(1000, 0)); e == nil {
		t.Fatal("expected intent failure")
	}
	j := f.job()
	if _, e := os.Stat(filepath.Join(f.library, j.Plan.Files[5].Target)); !os.IsNotExist(e) {
		t.Fatal("linked before intent committed")
	}
	records, e := f.store.FileExecutions(context.Background(), j.ID, j.Plan.Version)
	if e != nil || records[5].State != "pending" {
		t.Fatal(records, e)
	}
	if _, e = f.store.db.Exec("DROP TRIGGER reject_intent"); e != nil {
		t.Fatal(e)
	}
	f.reopen()
	f.tick(1001)
	if f.job().Import != "imported" {
		t.Fatal("intent recovery failed")
	}
}
func TestReadyEventFailureDoesNotLosePerVideoDiscovery(t *testing.T) {
	f := packFixture(t, "relative")
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	if _, e := f.store.db.Exec(`CREATE TRIGGER reject_ready BEFORE INSERT ON events WHEN NEW.kind='discovered' BEGIN SELECT RAISE(ABORT,'fixture'); END`); e != nil {
		t.Fatal(e)
	}
	if e := f.engine.Tick(context.Background(), time.Unix(1000, 0)); e == nil {
		t.Fatal("expected ready failure")
	}
	j := f.job()
	if j.Discovery == "confirmed" {
		t.Fatal("ready escaped rollback")
	}
	records, e := f.store.FileExecutions(context.Background(), j.ID, j.Plan.Version)
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, r := range records {
		if r.Video && r.Discovered {
			n++
		}
	}
	if n != 11 {
		t.Fatal("per-video progress lost")
	}
	if _, e = f.store.db.Exec("DROP TRIGGER reject_ready"); e != nil {
		t.Fatal(e)
	}
	f.reopen()
	f.tick(1001)
	if f.job().Discovery != "confirmed" {
		t.Fatal("ready recovery failed")
	}
	events, _ := f.store.Events(context.Background(), 0)
	n = 0
	for _, v := range events {
		if v.Kind == "discovered" {
			n++
		}
	}
	if n != 1 {
		t.Fatal("duplicate ready", n)
	}
}
func TestPartialPackDoesNotDelaySeedDeadline(t *testing.T) {
	f := packFixture(t, "relative")
	j := f.job()
	j.SeedDays = 7
	if e := f.store.Save(context.Background(), &j, "fixture_seed_policy"); e != nil {
		t.Fatal(e)
	}
	f.engine.Files = &faultFiles{FileOperations: f.files, at: 7, permission: true}
	if e := f.engine.Tick(context.Background(), time.Unix(1000, 0)); e == nil {
		t.Fatal("expected failure")
	}
	if f.job().Seed != "seeding" {
		t.Fatal("early stop")
	}
	f.engine.Files = &faultFiles{FileOperations: f.files, at: 1, permission: true}
	if e := f.engine.Tick(context.Background(), time.Unix(1000+7*86400, 0)); e == nil {
		t.Fatal("expected repeated failure")
	}
	j = f.job()
	if j.Seed != "stopped" || j.Import != "planned" {
		t.Fatal("partial import delayed stop")
	}
}
func FuzzTitleEvidenceBounds(f *testing.F) {
	for _, s := range []string{packTitle, "Example - 02.5", "[Example] [02]", "东京复仇者 三天战争篇 [02]", "Show S00E01"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, e := ParseTitle(s)
		if e != nil {
			return
		}
		for _, r := range []*EpisodeRange{p.Relative, p.Absolute} {
			if r != nil && (r.Start < 1 || r.End < r.Start || r.End > 999) {
				t.Fatal("range escaped bounds")
			}
		}
		if p.Season < 0 || p.Season > 99 {
			t.Fatal("season escaped bounds")
		}
	})
}
