package nas

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 在事件 append 处中断事务，覆盖真实 UPDATE 已执行而 COMMIT 未完成的窗口。
func TestExternalEffectThenTransactionFailure(t *testing.T) {
	for _, kind := range []string{"imported", "seed_stopped", "refresh_requested"} {
		t.Run(kind, func(t *testing.T) {
			days := 7
			if kind == "seed_stopped" {
				days = 0
			}
			f := newFixture(t, "Show.S01E01.mkv", days)
			ctx := context.Background()
			_, err := f.store.db.Exec(fmt.Sprintf(`CREATE TRIGGER fail_event BEFORE INSERT ON events WHEN NEW.kind='%s' BEGIN SELECT RAISE(ABORT,'fixture_failure'); END;`, kind))
			if err != nil {
				t.Fatal(err)
			}
			if err = f.engine.Tick(ctx, time.Unix(1000, 0)); err == nil {
				t.Fatal("expected transactional failure")
			}
			events, err := f.store.Events(ctx, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range events {
				if v.Kind == kind {
					t.Fatal("rolled back event became visible")
				}
			}
			j := f.job()
			if kind == "imported" && j.Import == "imported" {
				t.Fatal("state escaped transaction")
			}
			if _, err = f.store.db.Exec("DROP TRIGGER fail_event"); err != nil {
				t.Fatal(err)
			}
			f.reopen()
			f.tick(1001)
			f.tick(1002)
			events, _ = f.store.Events(ctx, 0)
			count := 0
			for _, v := range events {
				if v.Kind == kind {
					count++
				}
			}
			if count != 1 {
				t.Fatal("logical event duplicated", count)
			}
			a, err := os.Stat(filepath.Join(f.downloads, f.name))
			if err != nil {
				t.Fatal("source removed", err)
			}
			j = f.job()
			b, err := os.Stat(filepath.Join(f.library, j.Plan.Files[0].Target))
			if err != nil || !os.SameFile(a, b) {
				t.Fatal("hardlink recovery failed", err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if kind == "seed_stopped" && f.stops != 2 {
				t.Fatal("stop was not safely replayed", f.stops)
			}
			if kind == "refresh_requested" && f.refreshes != 2 {
				t.Fatal("refresh was not safely replayed", f.refreshes)
			}
		})
	}
}

func TestClockRollbackRestartAndNoSourceDeletion(t *testing.T) {
	f := newFixture(t, "unknown.mkv", 7)
	f.tick(1000)
	deadline := f.job().StopAt
	f.reopen()
	f.tick(900)
	if f.job().StopAt != deadline || f.job().Seed != "seeding" {
		t.Fatal("clock rollback moved deadline")
	}
	f.tick(deadline)
	f.reopen()
	f.tick(800)
	if f.job().Seed != "stopped" {
		t.Fatal("clock rollback resumed torrent")
	}
	if _, err := os.Stat(filepath.Join(f.downloads, f.name)); err != nil {
		t.Fatal("source removed", err)
	}
}

func TestDirectorySwapCannotFollowSymlink(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 7)
	fp, err := f.files.Fingerprint(f.name)
	if err != nil {
		t.Fatal(err)
	}
	slot := filepath.Join(f.library, "slot")
	parked := filepath.Join(f.library, "parked")
	outside := filepath.Join(f.root, "outside")
	for _, p := range []string{slot, outside} {
		if err = os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			os.Rename(slot, parked)
			os.Symlink(outside, slot)
			os.Remove(slot)
			os.Rename(parked, slot)
		}
	}()
	for i := 0; i < 100; i++ {
		_ = f.files.Link(PlannedFile{f.name, "slot/video.mkv", fp})
	}
	wg.Wait()
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("symlink escape", entries, err)
	}
}

func TestCancelledImportHasNoFilesystemEffect(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 7)
	p, err := f.files.Preview(f.name, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = f.files.LinkContext(ctx, p.Files[0]); err == nil {
		t.Fatal("ignored cancellation")
	}
	entries, err := os.ReadDir(f.library)
	if err != nil || len(entries) != 0 {
		t.Fatal("cancelled operation changed library")
	}
}

func TestConfigSizeLimitIncludesTrailingWhitespace(t *testing.T) {
	payload := `{"database":"/d/s.db","downloads":"/downloads","library":"/library","seed_days":0,"qb_url":"http://qb","emby_url":"http://emby"}` + strings.Repeat(" ", 65536)
	if _, err := LoadConfig(strings.NewReader(payload)); err == nil {
		t.Fatal("size limit bypassed")
	}
}
