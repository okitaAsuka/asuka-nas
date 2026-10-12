package nas

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileSafetyAndIdempotence(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 7)
	p, e := f.files.Preview(f.name, 1)
	if e != nil {
		t.Fatal(e)
	}
	for _, src := range []string{"../outside.mkv", "/etc/passwd", "sub/../Show.S01E01.mkv", "./Show.S01E01.mkv"} {
		if _, e = f.files.Fingerprint(src); e == nil {
			t.Fatal("unsafe source accepted", src)
		}
	}
	outside := filepath.Join(f.root, "outside")
	if e = os.Mkdir(outside, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(outside, filepath.Join(f.downloads, "escape")); e != nil {
		t.Fatal(e)
	}
	if _, e = f.files.Fingerprint("escape/file"); e == nil {
		t.Fatal("source symlink accepted")
	}
	if e = os.Symlink(filepath.Join(f.downloads, f.name), filepath.Join(f.downloads, "alias.mkv")); e != nil {
		t.Fatal(e)
	}
	if _, e = f.files.Fingerprint("alias.mkv"); e == nil {
		t.Fatal("leaf symlink accepted")
	}
	if e = os.Symlink(outside, filepath.Join(f.library, "escape")); e != nil {
		t.Fatal(e)
	}
	bad := p.Files[0]
	for _, dst := range []string{"../escape.mkv", "/tmp/escape.mkv", "escape/video.mkv"} {
		bad.Target = dst
		if e = f.files.Link(bad); e == nil {
			t.Fatal("unsafe target accepted", dst)
		}
	}
	if e = os.Mkdir(filepath.Join(f.downloads, "dir.mkv"), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = f.files.Fingerprint("dir.mkv"); e == nil {
		t.Fatal("directory accepted")
	}
	good := p.Files[0]
	if e = f.files.Link(good); e != nil {
		t.Fatal(e)
	}
	if e = f.files.Link(good); e != nil {
		t.Fatal("replay", e)
	}
	conflict := good
	conflict.Target = "existing.mkv"
	existing := filepath.Join(f.library, conflict.Target)
	if e = os.WriteFile(existing, []byte("preserve"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = f.files.Link(conflict); e == nil || e.Error() != "target_conflict" {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(existing)
	if string(b) != "preserve" {
		t.Fatal("overwritten")
	}
	if e = os.WriteFile(filepath.Join(f.downloads, f.name), []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = f.files.Link(good); e == nil || e.Error() != "fingerprint_changed" {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(f.downloads, f.name)); e != nil {
		t.Fatal("source deleted")
	}
	if other, e := OpenFiles(f.downloads, "/proc"); e == nil {
		other.Close()
		t.Fatal("cross filesystem accepted")
	} else if e.Error() != "cross_device" {
		t.Fatal(e)
	}
}

func TestStaleConfirmationAndSymlinkSidecar(t *testing.T) {
	f := newFixture(t, "unknown.mkv", 0)
	f.tick(1000)
	j := f.job()
	ctx := context.Background()
	p, e := f.engine.Propose(ctx, j.ID, f.name, "Show/Season 01/Show - S01E01.mkv")
	if e != nil {
		t.Fatal(e)
	}
	p2, e := f.engine.Propose(ctx, j.ID, f.name, "Show/Season 02/Show - S02E01.mkv")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.engine.Confirm(ctx, j.ID, p.Version, PlanDigest(p)); e == nil {
		t.Fatal("superseded plan accepted")
	}
	if e = os.WriteFile(filepath.Join(f.downloads, f.name), []byte("modified"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = f.engine.Confirm(ctx, j.ID, p2.Version, PlanDigest(p2)); e == nil {
		t.Fatal("stale fingerprint accepted")
	}
	if e = os.Symlink(filepath.Join(f.downloads, f.name), filepath.Join(f.downloads, "unknown.ass")); e != nil {
		t.Fatal(e)
	}
	if _, e = f.files.ProposedPlan(f.name, "Show.mkv", 3); e == nil {
		t.Fatal("symlink sidecar accepted")
	}
}

func TestConflictDoesNotBlockSeedDeadline(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 7)
	p, e := f.files.Preview(f.name, 1)
	if e != nil {
		t.Fatal(e)
	}
	target := filepath.Join(f.library, p.Files[0].Target)
	if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(target, []byte("existing"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = f.engine.Tick(context.Background(), time.Unix(1000, 0)); e == nil {
		t.Fatal("expected conflict")
	}
	f.reopen()
	if e = f.engine.Tick(context.Background(), time.Unix(1000+7*86400, 0)); e == nil {
		t.Fatal("expected conflict after restart")
	}
	j := f.job()
	if j.Seed != "stopped" || j.Import == "imported" {
		t.Fatalf("wrong state %+v", j)
	}
}

func TestConfirmationRechecksConflict(t *testing.T) {
	f := newFixture(t, "unknown.mkv", 0)
	f.tick(1000)
	j := f.job()
	ctx := context.Background()
	p, e := f.engine.Propose(ctx, j.ID, f.name, "Chosen.mkv")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(f.library, "Chosen.mkv"), []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = f.engine.Confirm(ctx, j.ID, p.Version, PlanDigest(p)); e == nil || e.Error() != "target_conflict" {
		t.Fatal(e)
	}
}
