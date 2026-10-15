package nas

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerticalLoopReplayAndIndependentSeeding(t *testing.T) {
	f := newFixture(t, "Show.S02E03.mkv", 7)
	f.manifest = []string{f.name, "Show.S02E03.zh-Hans.ass"}
	if e := os.WriteFile(filepath.Join(f.downloads, "Show.S02E03.zh-Hans.ass"), []byte("subtitle"), 0600); e != nil {
		t.Fatal(e)
	}
	f.tick(1000)
	j := f.job()
	if j.Import != "imported" || j.Refresh != "requested" || j.Discovery != "pending" || j.Seed != "seeding" {
		t.Fatalf("unexpected states: %+v", j)
	}
	if len(j.Plan.Files) != 2 {
		t.Fatal("missing sidecar")
	}
	for _, p := range j.Plan.Files {
		a, e := os.Stat(filepath.Join(f.downloads, p.Source))
		if e != nil {
			t.Fatal(e)
		}
		b, e := os.Stat(filepath.Join(f.library, p.Target))
		if e != nil || !os.SameFile(a, b) {
			t.Fatal("not hardlink", e)
		}
	}
	before, _ := f.store.Events(context.Background(), 0)
	f.reopen()
	if _, e := f.engine.IngestRSS(context.Background(), "fixture", strings.NewReader(f.rss()), 7); e != nil {
		t.Fatal(e)
	}
	f.tick(1001)
	after, _ := f.store.Events(context.Background(), 0)
	if len(before) != len(after) {
		t.Fatal("duplicate events")
	}
	f.mu.Lock()
	f.discovered = true
	f.completed = 9999
	f.mu.Unlock()
	f.tick(1000 + 7*86400 - 1)
	if f.job().Seed != "seeding" {
		t.Fatal("stopped early")
	}
	f.tick(1000 + 7*86400)
	j = f.job()
	if j.Seed != "stopped" || j.Discovery != "confirmed" || j.CompletedAt != 1000 {
		t.Fatalf("unexpected completion: %+v", j)
	}
	f.tick(1000 + 8*86400)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.adds != 1 || f.stops != 1 || f.refreshes != 1 {
		t.Fatal(f.adds, f.stops, f.refreshes)
	}
}

func TestUnknownSeasonStopsImmediatelyAndConfirmation(t *testing.T) {
	f := newFixture(t, "[group] Show - 01.mkv", 0)
	f.tick(1000)
	j := f.job()
	if j.Import != "needs_confirmation" || j.Seed != "stopped" || j.Plan != nil {
		t.Fatalf("guessed season or failed stop: %+v", j)
	}
	ctx := context.Background()
	p, e := f.engine.Propose(ctx, j.ID, f.name, "Show/Season 03/Show - S03E01.mkv")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.engine.Confirm(ctx, j.ID, p.Version-1, PlanDigest(p)); e == nil {
		t.Fatal("stale plan accepted")
	}
	if e = f.engine.Confirm(ctx, j.ID, p.Version, "wrong"); e == nil {
		t.Fatal("wrong digest accepted")
	}
	if e = f.engine.Confirm(ctx, j.ID, p.Version, PlanDigest(p)); e != nil {
		t.Fatal(e)
	}
	f.tick(1001)
	if f.job().Import != "imported" {
		t.Fatal("not imported")
	}
}

func TestRefreshRetryAndCrashAfterLink(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 0)
	ctx := context.Background()
	j := f.job()
	// 模拟完成已落库、硬链接已成功，但 imported 事务前进程崩溃。
	p, e := f.files.Preview(f.name, 2)
	if e != nil {
		t.Fatal(e)
	}
	p.Approved = true
	p.ContextVersion = j.ContextVersion
	if e = f.engine.bindManualPlan(ctx, j, p); e != nil {
		t.Fatal(e)
	}
	j.Plan = p
	j.Download = "completed"
	j.CompletedAt = 1000
	j.StopAt = 1000
	j.Seed = "seeding"
	j.Import = "planned"
	j.Sources = []string{f.name}
	if e = f.store.Save(ctx, &j, "plan_ready"); e != nil {
		t.Fatal(e)
	}
	if e = f.files.Link(p.Files[0]); e != nil {
		t.Fatal(e)
	}
	f.reopen()
	f.mu.Lock()
	f.failRefresh = true
	f.mu.Unlock()
	if e = f.engine.Tick(ctx, time.Unix(1000, 0)); e == nil {
		t.Fatal("expected refresh failure")
	}
	j = f.job()
	if j.Import != "imported" || j.Refresh != "pending" || j.Seed != "stopped" {
		t.Fatalf("bad recovery: %+v", j)
	}
	f.mu.Lock()
	f.failRefresh = false
	f.mu.Unlock()
	f.reopen()
	f.tick(1001)
	if f.job().Refresh != "requested" {
		t.Fatal("refresh not retried")
	}
}

func TestFilesFailureDoesNotPreventStop(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 0)
	f.mu.Lock()
	f.failFiles = true
	f.mu.Unlock()
	if e := f.engine.Tick(context.Background(), time.Unix(1000, 0)); e == nil {
		t.Fatal("expected files failure")
	}
	if f.job().Seed != "stopped" {
		t.Fatal("files failure blocked stop")
	}
}

func TestDownloadPathMismatchStillStops(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 0)
	f.mu.Lock()
	f.wrongRoot = true
	f.mu.Unlock()
	if err := f.engine.Tick(context.Background(), time.Unix(1000, 0)); err == nil {
		t.Fatal("expected path mismatch")
	}
	j := f.job()
	if j.Seed != "stopped" || j.Import == "imported" {
		t.Fatal("path mismatch blocked stop or imported")
	}
}
