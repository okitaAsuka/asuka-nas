package nas

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func libraryFile(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func seedLibrary(t *testing.T, root string) {
	libraryFile(t, root, "Example/tvshow.nfo", `<tvshow><title>Example</title><uniqueid type="tmdb">untrusted-id</uniqueid></tvshow>`)
	libraryFile(t, root, "Example/Season 01/Example - S01E01.mkv", "never read video content")
	libraryFile(t, root, "Example/Season 01/Example - S01E01.nfo", `<episodedetails><showtitle>Example</showtitle><season>1</season><episode>1</episode><absolute_number>67</absolute_number></episodedetails>`)
}
func TestLibraryEvidenceCandidates(t *testing.T) {
	root := t.TempDir()
	seedLibrary(t, root)
	ctx := context.Background()
	a, e := discoverLibrary(ctx, root, "Example")
	if e != nil {
		t.Fatal(e)
	}
	b, e := discoverLibrary(ctx, root, "Example")
	if e != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("nondeterministic", e, a, b)
	}
	c := a.Candidates[0]
	if c.Season != 1 || c.DisplayName != "Example" || !reflect.DeepEqual(c.Episodes, []int{1}) || len(c.Sources) != 3 || len(c.Conflicts) != 0 || !strings.HasPrefix(c.SeriesID, "library-") {
		t.Fatal(c)
	}
	absoluteFound := false
	for _, ev := range c.Sources {
		if ev.Absolute == 67 {
			absoluteFound = true
		}
	}
	if !absoluteFound {
		t.Fatal("lost paired absolute evidence", c)
	}
	libraryFile(t, root, "Example/Season 01/Example - S01E01.nfo", `<episodedetails><showtitle>Different</showtitle><season>2</season><episode>3</episode></episodedetails>`)
	b, e = discoverLibrary(ctx, root, "Example")
	if e != nil || a.Digest == b.Digest || len(b.Candidates[0].Conflicts) < 2 {
		t.Fatal(b, e)
	}
	libraryFile(t, root, "Unknown/第四季/tvshow.nfo", `<tvshow><title>Unknown Arc</title></tvshow>`)
	unknown, e := discoverLibrary(ctx, root, "Unknown")
	if e != nil || unknown.Candidates[0].Season != 0 || len(unknown.Candidates[0].Episodes) != 0 {
		t.Fatal("folder guessed season", unknown, e)
	}
}
func TestLibrarySafetyLimitsAndCancellation(t *testing.T) {
	cases := []struct {
		name            string
		setup           func(*testing.T, string)
		directory, code string
	}{
		{"escape", func(t *testing.T, r string) {}, "../outside", "unsafe_path"},
		{"absolute", func(t *testing.T, r string) {}, "/tmp", "unsafe_path"},
		{"root selection", func(t *testing.T, r string) {}, ".", "unsafe_path"},
		{"symlink", func(t *testing.T, r string) {
			libraryFile(t, r, "Show/tvshow.nfo", "<tvshow/>")
			if e := os.Symlink(t.TempDir(), filepath.Join(r, "Show/out")); e != nil {
				t.Fatal(e)
			}
		}, "Show", "library_unsafe_path"},
		{"symlink selection", func(t *testing.T, r string) {
			if e := os.Symlink(t.TempDir(), filepath.Join(r, "Show")); e != nil {
				t.Fatal(e)
			}
		}, "Show", "library_unsafe_path"},
		{"too large", func(t *testing.T, r string) { libraryFile(t, r, "Show/a.nfo", strings.Repeat("x", libraryNFOLimit+1)) }, "Show", "library_limit"},
		{"total bytes", func(t *testing.T, r string) {
			for i := range 17 {
				libraryFile(t, r, fmt.Sprintf("Show/%02d.nfo", i), "<tvshow><plot>"+strings.Repeat("x", 63000)+"</plot></tvshow>")
			}
		}, "Show", "library_limit"},
		{"too deep", func(t *testing.T, r string) { libraryFile(t, r, "Show/a/b/c/d/a.nfo", "<tvshow/>") }, "Show", "library_limit"},
		{"too many", func(t *testing.T, r string) {
			for i := range libraryEntryLimit + 1 {
				libraryFile(t, r, fmt.Sprintf("Show/%03d.txt", i), "")
			}
		}, "Show", "library_limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)
			v, e := discoverLibrary(context.Background(), root, tc.directory)
			if e == nil || e.Error() != tc.code || len(v.Candidates) != 0 {
				t.Fatal(v, e)
			}
		})
	}
	root := t.TempDir()
	seedLibrary(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := discoverLibrary(ctx, root, "Example"); e == nil || e.Error() != "library_cancelled" {
		t.Fatal(e)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, e := discoverLibrary(ctx, root, "Example"); e == nil || e.Error() != "library_cancelled" {
		t.Fatal(e)
	}
	// 50GiB sparse video 仅做 stat；不会触发 NFO 字节限制或视频哈希。
	p := filepath.Join(root, "Example", "Example - S02E01.mkv")
	file, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	e = file.Truncate(50 << 30)
	file.Close()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = discoverLibrary(context.Background(), root, "Example"); e != nil {
		t.Fatal(e)
	}
}
func TestLibraryXMLUntrusted(t *testing.T) {
	invalid := []string{
		`<!DOCTYPE tvshow [<!ENTITY x SYSTEM "file:///etc/passwd">]><tvshow><title>&x;</title></tvshow>`,
		`<!DOCTYPE tvshow SYSTEM "https://example.invalid/private"><tvshow/>`,
		`<tvshow><title>&unknown;</title></tvshow>`,
		`<tvshow><title><b>nested</b></title></tvshow>`,
		`<tvshow><title>One</title><title>Two</title></tvshow>`,
		`<tvshow/><tvshow/>`, `<tvshow/>trailing`, `<tvshow><?external url?></tvshow>`,
		`<episodedetails><season>0</season><episode>1</episode></episodedetails>`,
		`<episodedetails><season>1</season><episode>1.5</episode></episodedetails>`,
		`<tvshow><title>../escape</title></tvshow>`,
		`<tvshow>` + strings.Repeat("<nested>", 17) + strings.Repeat("</nested>", 17) + `</tvshow>`,
		`<tvshow>` + strings.Repeat("<ignored/>", 2100) + `</tvshow>`,
	}
	for i, s := range invalid {
		if _, e := parseLibraryNFO([]byte(s)); e == nil {
			t.Fatalf("invalid XML accepted %d", i)
		}
	}
	root := t.TempDir()
	libraryFile(t, root, "Show/invalid.nfo", invalid[0])
	libraryFile(t, root, "Show/Show - S01E01.mkv", "")
	c, e := discoverLibrary(context.Background(), root, "Show")
	if e != nil || len(c.Candidates[0].Conflicts) != 1 {
		t.Fatal(c, e)
	}
	b, _ := json.Marshal(c)
	if strings.Contains(string(b), "passwd") || strings.Contains(string(b), "SYSTEM") {
		t.Fatal("raw XML leaked")
	}
}
func TestLibraryAbsoluteEvidenceConflict(t *testing.T) {
	root := t.TempDir()
	for i := 1; i <= 2; i++ {
		libraryFile(t, root, fmt.Sprintf("Show/%d.nfo", i), fmt.Sprintf(`<episodedetails><showtitle>Show</showtitle><season>1</season><episode>%d</episode><absolute_number>67</absolute_number></episodedetails>`, i))
	}
	c, e := discoverLibrary(context.Background(), root, "Show")
	if e != nil || !reflect.DeepEqual(c.Candidates[0].Conflicts, []string{"absolute_episode_conflict"}) {
		t.Fatal(c, e)
	}
}
func TestCandidateRuleFreshnessAndRecovery(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "Example [02].mkv", -1)
	seedLibrary(t, f.library)
	m, web := managementFixture(t, f)
	m.Daemon.Config.Library = f.library
	id, e := f.store.IngestRelease(ctx, "fixture", "guid", f.hash, "magnet:?xt=urn:btih:"+f.hash, nil, 0, "Example Arc [02]")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.engine.Tick(ctx, time.Unix(1001, 0)); e != nil {
		t.Fatal(e)
	}
	if j := f.job(); j.Import != "needs_confirmation" || j.Plan != nil {
		t.Fatal(j)
	}
	status, b := postManagement(t, m, web, "library-candidates", map[string]any{"directory": "Example"})
	if status != 200 {
		t.Fatal(status, string(b))
	}
	var candidate LibraryCandidates
	if e = json.Unmarshal(b, &candidate); e != nil {
		t.Fatal(e)
	}
	rule := NamingRule{ID: "arc", Revision: 1, SeriesID: candidate.Candidates[0].SeriesID, Provider: "manual", Order: "custom", Subscription: "fixture", Aliases: []string{"Example Arc"}, Season: 1, DisplayName: "Example", Directory: "Example"}
	request := map[string]any{"directory": "Example", "digest": candidate.Digest, "rule": rule, "title": "Example Arc [02]", "source": f.name}
	if status, b = postManagement(t, m, web, "candidate-rule-preview", request); status != 200 {
		t.Fatal(status, string(b))
	}
	libraryFile(t, f.library, "Example/new.txt", "changed")
	if status, b = postManagement(t, m, web, "candidate-rule", request); status != 409 || !strings.Contains(string(b), "candidate_stale") {
		t.Fatal(status, string(b))
	}
	rules, _ := f.store.RuleViews(ctx)
	if len(rules) != 0 {
		t.Fatal("stale candidate saved")
	}
	fresh, e := discoverLibrary(ctx, f.library, "Example")
	if e != nil {
		t.Fatal(e)
	}
	request["digest"] = fresh.Digest
	if status, b = postManagement(t, m, web, "candidate-rule", request); status != 200 {
		t.Fatal(status, string(b))
	}
	if e = f.engine.Tick(ctx, time.Unix(1002, 0)); e != nil {
		t.Fatal(e)
	}
	if f.job().Import != "needs_confirmation" {
		t.Fatal("draft authorized execution")
	}
	epoch, _ := f.store.NamingEpoch(ctx)
	if status, b = postManagement(t, m, web, "rule-state", map[string]any{"id": "arc", "revision": 1, "epoch": epoch, "enabled": true}); status != 200 {
		t.Fatal(status, string(b))
	}
	if e = f.engine.Tick(ctx, time.Unix(1003, 0)); e != nil {
		t.Fatal(e)
	}
	j := f.job()
	if j.Import != "imported" {
		t.Fatal(j)
	}
	src, _ := os.Stat(filepath.Join(f.downloads, f.name))
	dst, e := os.Stat(filepath.Join(f.library, j.Plan.Files[0].Target))
	if e != nil || !os.SameFile(src, dst) {
		t.Fatal("not hardlink", e)
	}
	frozen := PlanDigest(j.Plan)
	rule.Revision = 2
	rule.Directory = "AnotherDirectory"
	if e = f.store.AddRule(ctx, rule); e != nil {
		t.Fatal(e)
	}
	if e = f.store.SetRule(ctx, rule.ID, 2, true); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	if e = f.engine.Tick(ctx, time.Unix(1004, 0)); e != nil {
		t.Fatal(e)
	}
	if j = f.job(); j.Discovery != "confirmed" || PlanDigest(j.Plan) != frozen {
		t.Fatal("frozen plan changed", j)
	}
	events, e := f.store.Events(ctx, 0)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, v := range events {
		if v.JobID == id && v.Kind == "discovered" {
			found = true
		}
	}
	if !found || f.adds != 1 {
		t.Fatal("event or dedup", f.adds)
	}
}
