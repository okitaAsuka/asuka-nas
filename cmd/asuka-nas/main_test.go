package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// 子进程走真实 CLI 和 signal.NotifyContext，不用 fake context 代替 SIGTERM。
func TestDaemonSIGTERM(t *testing.T) {
	if os.Getenv("ASUKA_NAS_TEST_CHILD") == "1" {
		flag.CommandLine = flag.NewFlagSet("asuka-nas", flag.ExitOnError)
		os.Args = []string{"asuka-nas", "-run", "-config", os.Getenv("ASUKA_NAS_TEST_CONFIG")}
		if err := run(); err != nil {
			fmt.Fprintln(os.Stderr, "child_failed")
			os.Exit(2)
		}
		os.Exit(0)
	}
	root := t.TempDir()
	downloads := filepath.Join(root, "downloads")
	library := filepath.Join(root, "library")
	for _, p := range []string{downloads, library} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	cfg := map[string]any{"database": filepath.Join(root, "state.db"), "downloads": downloads, "library": library, "seed_days": 0, "qb_url": server.URL, "emby_url": server.URL, "tick_seconds": 1, "request_timeout_seconds": 60, "subscriptions": []any{map[string]any{"id": "fixture", "url": server.URL + "/rss?token=fixture-secret"}}}
	b, _ := json.Marshal(cfg)
	path := filepath.Join(root, "config.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestDaemonSIGTERM$")
	// 测试子进程不继承宿主 qB 凭据，仅传递测试入口变量。
	child.Env = []string{"ASUKA_NAS_TEST_CHILD=1", "ASUKA_NAS_TEST_CONFIG=" + path}
	var output bytes.Buffer
	child.Stdout = &output
	child.Stderr = &output
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("early exit: %v %s", err, output.String())
	case <-time.After(5 * time.Second):
		child.Process.Kill()
		<-done
		t.Fatal("child did not start")
	}
	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v %s", err, output.String())
		}
	case <-time.After(5 * time.Second):
		child.Process.Kill()
		<-done
		t.Fatal("SIGTERM did not cancel request")
	}
	if strings.Contains(output.String(), "fixture-secret") || strings.Contains(output.String(), server.URL) {
		t.Fatal("secret leaked")
	}
}
