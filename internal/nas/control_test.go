package nas

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWholeManifestBlocksCollectionsAndAmbiguousSubtitles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  []string
		reason string
	}{
		{"collection", []string{"Show.S01E01.mkv", "Show.S01E02.mkv"}, "multiple_or_missing_videos"},
		{"mixed_video_formats", []string{"Show.S01E01.mkv", "Show.S01E02.avi"}, "unsupported_video"},
		{"unknown_season", []string{"Show - 01.mkv"}, "needs_confirmation"},
		{"unknown_episode", []string{"Show.S01.mkv"}, "needs_confirmation"},
		{"subtitle_wrong_stem", []string{"Show.S01E01.mkv", "episode.ass"}, "subtitle_mapping_ambiguous"},
		{"subtitle_other_directory", []string{"Show.S01E01.mkv", "subs/Show.S01E01.ass"}, "subtitle_mapping_ambiguous"},
		{"bitmap_subtitle", []string{"Show.S01E01.mkv", "Show.S01E01.sup"}, "subtitle_mapping_ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.files[0], 0)
			f.manifest = tc.files
			f.tick(1000)
			j := f.job()
			if j.Import != "needs_confirmation" || j.ErrorCode != tc.reason || j.BlockReason != tc.reason || j.Seed != "stopped" || j.Plan != nil {
				t.Fatalf("bad blocked state: %+v", j)
			}
			entries, err := os.ReadDir(f.library)
			if err != nil || len(entries) != 0 {
				t.Fatal("partial collection imported", err)
			}
			if len(tc.files) > 1 {
				if _, err = f.engine.Propose(context.Background(), j.ID, tc.files[0], "Chosen.mkv"); err == nil {
					t.Fatal("manual single-file override bypassed whole manifest")
				}
			}
			if _, err = os.Stat(filepath.Join(f.downloads, tc.files[0])); err != nil {
				t.Fatal("source removed", err)
			}
		})
	}
}

func TestManifestExcludesOtherJobsSidecars(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 0)
	for _, name := range []string{"Other.S02E03.ass", "Show.S01E01.ass"} {
		if err := os.WriteFile(filepath.Join(f.downloads, name), []byte("another job's sidecar"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.tick(1000)
	j := f.job()
	if j.Import != "imported" || len(j.Plan.Files) != 1 {
		t.Fatal("unlisted sidecar imported")
	}
}

func TestIdleJobsAndSubscriptionsDoNotWriteOrRequest(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 0)
	f.discovered = true
	f.tick(1000)
	ctx := context.Background()
	if _, err := f.store.ClaimSource(ctx, "idle", 1000, 600); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := f.store.db.QueryRow("SELECT total_changes()").Scan(&before); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	calls := f.requests
	f.mu.Unlock()
	events, err := f.store.Events(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := int64(1001); i < 1011; i++ {
		if err = f.engine.RetryTick(ctx, time.Unix(i, 0), 15); err != nil {
			t.Fatal(err)
		}
		if due, err := f.store.ClaimSource(ctx, "idle", i, 600); err != nil || due {
			t.Fatal(due, err)
		}
	}
	if err = f.store.db.QueryRow("SELECT total_changes()").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("idle database writes", before, after)
	}
	tail, err := f.store.Events(ctx, events[len(events)-1].ID)
	if err != nil || len(tail) != 0 {
		t.Fatal("idle events", tail, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.requests != calls {
		t.Fatal("idle external operations")
	}
}

func TestDownloadRootMustMatchAndFilesMustExistLocally(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 0)
	if same, err := OpenFiles(f.downloads, f.downloads); err == nil {
		same.Close()
		t.Fatal("same root accepted")
	}
	if err := os.Remove(filepath.Join(f.downloads, f.name)); err != nil {
		t.Fatal(err)
	}
	if err := f.engine.Tick(context.Background(), time.Unix(1000, 0)); err == nil {
		t.Fatal("wrong physical mapping accepted")
	}
	j := f.job()
	if j.Import == "imported" || j.Seed != "stopped" {
		t.Fatal("mapping failure imported or blocked stop")
	}
}
