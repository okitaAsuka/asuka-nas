package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"github.com/okitaAsuka/asuka-nas/internal/nas"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 真实第二进程持锁失败：schema、表和内容均不得改变，覆盖 daemon/serve/rules 三条入口。
func TestStartupLockBeforeMigration(t *testing.T) {
	if os.Getenv("NAS_LOCK_CHILD") == "1" {
		flag.CommandLine = flag.NewFlagSet("nas", flag.ContinueOnError)
		path := os.Getenv("NAS_LOCK_CONFIG")
		switch os.Getenv("NAS_LOCK_MODE") {
		case "rules":
			os.Args = []string{"nas", "rules", "enable", "-id", "absent", "-config", path}
		case "serve":
			os.Args = []string{"nas", "-serve", "-config", path}
		default:
			os.Args = []string{"nas", "-run", "-config", path}
		}
		if run() != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	root := t.TempDir()
	dbpath := filepath.Join(root, "state.db")
	s, err := nas.OpenStore(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := sql.Open("sqlite", dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("DROP TABLE legacy_plan_evidence; PRAGMA user_version=4; CREATE TABLE lock_sentinel(value TEXT); INSERT INTO lock_sentinel VALUES('unchanged')"); err != nil {
		t.Fatal(err)
	}
	lock, err := nas.AcquireInstance(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	cfg := nas.Config{Database: dbpath, Downloads: filepath.Join(root, "missing-downloads"), Library: filepath.Join(root, "missing-library"), QBURL: "http://127.0.0.1:1", EmbyURL: "http://127.0.0.1:1"}
	b, _ := json.Marshal(cfg)
	path := filepath.Join(root, "config.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"run", "serve", "rules"} {
		t.Run(mode, func(t *testing.T) {
			child := exec.Command(os.Args[0], "-test.run=^TestStartupLockBeforeMigration$")
			child.Env = []string{"NAS_LOCK_CHILD=1", "NAS_LOCK_CONFIG=" + path, "NAS_LOCK_MODE=" + mode}
			err := child.Run()
			if ex, ok := err.(*exec.ExitError); !ok || ex.ExitCode() != 3 {
				t.Fatalf("expected failed second instance: %v", err)
			}
			var schema, tables int
			var value string
			db.QueryRow("PRAGMA user_version").Scan(&schema)
			db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='legacy_plan_evidence'").Scan(&tables)
			db.QueryRow("SELECT value FROM lock_sentinel").Scan(&value)
			if schema != 4 || tables != 0 || value != "unchanged" {
				t.Fatalf("mutated under held lock: %d %d %s", schema, tables, value)
			}
		})
	}
}

func TestStartupCapacityErrorIsSafe(t *testing.T) {
	for _, code := range []string{"subscription_limit", "subscription_history_limit"} {
		if got := startupMessage(errors.New(code)); got != "startup_failed: "+code {
			t.Fatal(got)
		}
	}
	if got := startupMessage(errors.New("http://127.0.0.1/rss?token=private")); got != "startup_failed" {
		t.Fatal("raw startup error leaked")
	}
}
