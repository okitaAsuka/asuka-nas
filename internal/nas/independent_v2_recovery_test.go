package nas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type reviewWorkerConfig struct {
	Index                                                             int
	Mode, DB, ID, Input, Result, Ready, Gate, Downloads, Library, URL string
}
type reviewWorkerResult struct{ Error string }
type reviewExitFiles struct {
	FileOperations
	calls int
}

func (f *reviewExitFiles) LinkContext(ctx context.Context, p PlannedFile) error {
	if err := f.FileOperations.LinkContext(ctx, p); err != nil {
		return err
	}
	f.calls++
	if f.calls == 5 {
		os.Exit(86)
	}
	return nil
}

// 真实独立子进程：不绕过文件 API，不修改生产代码。第5次真实硬链接后立即退出。
func TestIndependentV2ProcessWorker(t *testing.T) {
	config := os.Getenv("NAS_REVIEW_WORKER")
	if config == "" {
		t.Skip("subprocess only")
	}
	b, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	var c reviewWorkerConfig
	if err = json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(c.DB)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if c.Mode == "crash" {
		files, err := OpenFiles(c.Downloads, c.Library)
		if err != nil {
			t.Fatal(err)
		}
		defer files.Close()
		e := &Engine{Store: s, Files: &reviewExitFiles{FileOperations: files}, QB: &QB{HTTPBoundary: NewHTTPBoundary(c.URL, ""), Downloads: c.Downloads}, Emby: &Emby{NewHTTPBoundary(c.URL, "fake-token")}, Library: c.Library}
		if err = e.Tick(ctx, time.Unix(1000, 0)); err != nil {
			t.Fatal(err)
		}
		t.Fatal("crash point not reached")
	}
	b, err = os.ReadFile(c.Input)
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err = json.Unmarshal(b, &j); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(c.Ready, []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err = os.Stat(c.Gate); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("barrier timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if c.Mode == "reserve" {
		err = s.Save(ctx, &j, "plan_ready")
	} else {
		err = s.MarkFile(ctx, &j, c.Index, "file_intent")
	}
	result := reviewWorkerResult{}
	if err != nil {
		result.Error = err.Error()
	}
	b, _ = json.Marshal(result)
	if err = os.WriteFile(c.Result, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func reviewWriteJSON(t *testing.T, p string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func reviewProcess(t *testing.T, c reviewWorkerConfig) *exec.Cmd {
	t.Helper()
	config := filepath.Join(t.TempDir(), "worker.json")
	reviewWriteJSON(t, config, c)
	cmd := exec.Command(os.Args[0], "-test.run=^TestIndependentV2ProcessWorker$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "NAS_REVIEW_WORKER="+config)
	return cmd
}

func TestIndependentV2MultiprocessReservationAndCAS(t *testing.T) {
	for _, mode := range []string{"reserve", "file_intent"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, "Example.S09E02.mkv", 0)
			ctx := context.Background()
			j := f.job()
			if err := os.WriteFile(filepath.Join(f.downloads, "Example.S09E02.ass"), []byte("subtitle"), 0600); err != nil {
				t.Fatal(err)
			}
			p, err := f.files.Preview(f.name, j.Version+1)
			if err != nil {
				t.Fatal(err)
			}
			p.ContextVersion = j.ContextVersion
			j.Plan = p
			j.Import = "planned"
			jobs := []Job{j, j}
			if mode == "reserve" {
				id, err := f.store.Ingest(ctx, "fixture", "other", strings.Repeat("c", 40), "magnet", 0)
				if err != nil {
					t.Fatal(err)
				}
				other, err := f.store.Get(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				copyPlan := *p
				copyPlan.Version = other.Version + 1
				other.Plan = &copyPlan
				other.Import = "planned"
				jobs[1] = other
			} else {
				if err = f.store.Save(ctx, &j, "plan_ready"); err != nil {
					t.Fatal(err)
				}
				j.Plan.Started = true
				if err = f.store.Save(ctx, &j, "import_started"); err != nil {
					t.Fatal(err)
				}
				jobs = []Job{j, j}
			}
			root := t.TempDir()
			gate := filepath.Join(root, "gate")
			var commands []*exec.Cmd
			var configs []reviewWorkerConfig
			for i, job := range jobs {
				c := reviewWorkerConfig{Index: i, Mode: mode, DB: f.db, Input: filepath.Join(root, fmt.Sprintf("job%d.json", i)), Result: filepath.Join(root, fmt.Sprintf("result%d", i)), Ready: filepath.Join(root, fmt.Sprintf("ready%d", i)), Gate: gate}
				reviewWriteJSON(t, c.Input, job)
				cmd := reviewProcess(t, c)
				if err = cmd.Start(); err != nil {
					t.Fatal(err)
				}
				commands = append(commands, cmd)
				configs = append(configs, c)
				deadline := time.Now().Add(10 * time.Second)
				for {
					if _, e := os.Stat(c.Ready); e == nil {
						break
					}
					if time.Now().After(deadline) {
						cmd.Process.Kill()
						cmd.Wait()
						t.Fatal("worker initialization timeout")
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
			defer func() {
				for _, cmd := range commands {
					if cmd.ProcessState == nil {
						cmd.Process.Kill()
						cmd.Wait()
					}
				}
			}()
			deadline := time.Now().Add(10 * time.Second)
			for {
				ready := true
				for _, c := range configs {
					if _, err = os.Stat(c.Ready); err != nil {
						ready = false
					}
				}
				if ready {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("workers not ready")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if err = os.WriteFile(gate, []byte("go"), 0600); err != nil {
				t.Fatal(err)
			}
			successes := 0
			loser := -1
			for i, cmd := range commands {
				if err = cmd.Wait(); err != nil {
					t.Fatal(err)
				}
				b, err := os.ReadFile(configs[i].Result)
				if err != nil {
					t.Fatal(err)
				}
				var result reviewWorkerResult
				if err = json.Unmarshal(b, &result); err != nil {
					t.Fatal(err)
				}
				if result.Error == "" {
					successes++
				} else {
					loser = i
					t.Log("loser rejected:", result.Error)
				}
			}
			if successes != 1 {
				t.Fatalf("wanted one transaction winner, got %d", successes)
			}
			if mode == "reserve" {
				if err = f.store.Save(ctx, &jobs[loser], "plan_ready"); err == nil || err.Error() != "target_reserved" {
					t.Fatal("retry bypassed reservation", err)
				}
			} else {
				var intended int
				if err = f.store.db.QueryRow("SELECT COUNT(*) FROM file_execution WHERE state='intended'").Scan(&intended); err != nil || intended != 1 {
					t.Fatal("CAS failed to roll back file row", intended, err)
				}
			}
			kind := "plan_ready"
			if mode == "file_intent" {
				kind = "file_intent"
			}
			var count int
			if err = f.store.db.QueryRow("SELECT COUNT(*) FROM events WHERE kind=?", kind).Scan(&count); err != nil || count != 1 {
				t.Fatal("event not atomic", count, err)
			}
			for _, table := range []string{"target_reservations", "file_execution", "plan_history"} {
				want := 2
				if table == "plan_history" {
					want = 1
				}
				if err = f.store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
					t.Fatal("partial transaction", table, count, err)
				}
			}
		})
	}
}

type reviewSelectiveMedia struct {
	Media
	visible map[string]bool
	calls   map[string]int
}

func (m *reviewSelectiveMedia) Discover(_ context.Context, p string) (bool, error) {
	m.calls[p]++
	return m.visible[p], nil
}
func TestIndependentV2ProcessCrashFrozenEvidenceAndPartialDiscovery(t *testing.T) {
	for _, late := range []string{"identical", "conflict"} {
		t.Run(late, func(t *testing.T) {
			f := packFixture(t, "mixed")
			ctx := context.Background()
			cmd := reviewProcess(t, reviewWorkerConfig{Mode: "crash", DB: f.db, Downloads: f.downloads, Library: f.library, URL: f.server.URL})
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 86 {
				t.Fatalf("did not crash after real link: %v %s", err, output)
			}
			before := f.job()
			if before.Import != "planned" || !before.Plan.Started || before.Seed != "stopped" {
				t.Fatal(before)
			}
			records, err := f.store.FileExecutions(ctx, before.ID, before.Plan.Version)
			if err != nil {
				t.Fatal(err)
			}
			if records[3].State != "linked" || records[4].State != "intended" || records[5].State != "pending" {
				t.Fatal(records)
			}
			fifth := filepath.Join(f.library, before.Plan.Files[4].Target)
			inode, err := os.Stat(fifth)
			if err != nil {
				t.Fatal(err)
			}
			digest := PlanDigest(before.Plan)
			r := packRule()
			r.Revision = 2
			r.Directory = "FutureOnly"
			installRule(t, f.store, r)
			title := packTitle
			if late == "conflict" {
				title = strings.ReplaceAll(title, "01～11", "01～10")
			}
			addRelease(t, f, "fixture", "late", title)
			f.reopen()
			media := &reviewSelectiveMedia{Media: f.engine.Emby, visible: map[string]bool{}, calls: map[string]int{}}
			visible := 0
			for _, p := range before.Plan.Files {
				if videoTarget(p.Target) && visible < 5 {
					media.visible[filepath.Join(f.library, p.Target)] = true
					visible++
				}
			}
			f.engine.Emby = media
			f.tick(1001)
			after := f.job()
			if late == "conflict" {
				if after.BlockReason != "late_mapping_conflict" || after.Import != "planned" {
					t.Fatal(after)
				}
				if _, err = os.Stat(filepath.Join(f.library, before.Plan.Files[5].Target)); !os.IsNotExist(err) {
					t.Fatal("continued contradictory frozen plan")
				}
				return
			}
			if after.Import != "imported" || after.BlockReason != "" || after.Discovery == "confirmed" || PlanDigest(after.Plan) != digest {
				t.Fatal("recovery or partial discovery state", after)
			}
			inode2, err := os.Stat(fifth)
			if err != nil || !os.SameFile(inode, inode2) {
				t.Fatal("replaced existing link", err)
			}
			f.reopen()
			media.Media = f.engine.Emby
			f.engine.Emby = media
			prior := map[string]int{}
			already := map[string]bool{}
			for p, v := range media.visible {
				already[p] = v
			}
			for p, n := range media.calls {
				prior[p] = n
			}
			for _, p := range after.Plan.Files {
				if videoTarget(p.Target) {
					media.visible[filepath.Join(f.library, p.Target)] = true
				}
				if _, err = os.Stat(filepath.Join(f.downloads, p.Source)); err != nil {
					t.Fatal("source removed", err)
				}
			}
			f.tick(1002)
			if f.job().Discovery != "confirmed" {
				t.Fatal("all videos not ready")
			}
			for p, n := range prior {
				want := n + 1
				if already[p] {
					want = n
				}
				if media.calls[p] != want {
					t.Fatal("unexpected discovery retries")
				}
			}
			events, err := f.store.Events(ctx, 0)
			if err != nil {
				t.Fatal(err)
			}
			ready := 0
			linked := 0
			videos := 0
			for _, event := range events {
				switch event.Kind {
				case "discovered":
					ready++
				case "file_linked":
					linked++
				case "video_discovered":
					videos++
				}
			}
			if ready != 1 || linked != 22 || videos != 11 {
				t.Fatal("duplicate or missing event", ready, linked, videos)
			}
			if _, err = os.Stat(filepath.Join(f.library, "FutureOnly")); !os.IsNotExist(err) {
				t.Fatal("changed frozen target")
			}
		})
	}
}
