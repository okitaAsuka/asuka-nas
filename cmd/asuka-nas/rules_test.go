package main

import (
	"context"
	"encoding/json"
	"github.com/okitaAsuka/asuka-nas/internal/nas"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalRuleLifecycleAndDaemonLock(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config.json")
	database := filepath.Join(root, "state.db")
	cfg := map[string]any{"database": database, "downloads": filepath.Join(root, "downloads"), "library": filepath.Join(root, "library"), "seed_days": 0, "qb_url": "http://127.0.0.1:1", "emby_url": "http://127.0.0.1:1"}
	b, _ := json.Marshal(cfg)
	if e := os.WriteFile(config, b, 0600); e != nil {
		t.Fatal(e)
	}
	rule := nas.NamingRule{ID: "test", Revision: 1, SeriesID: "series", Provider: "manual", Order: "custom", Subscription: "fixture", Aliases: []string{"Fixture"}, Season: 9, Directory: "Chosen", DisplayName: "Fixture"}
	b, _ = json.Marshal(rule)
	file := filepath.Join(root, "rule.json")
	if e := os.WriteFile(file, b, 0600); e != nil {
		t.Fatal(e)
	}
	call := func(action string, extra ...string) error {
		return rulesCLI(append([]string{action, "-config", config}, extra...))
	}
	if e := call("add", "-file", file); e != nil {
		t.Fatal(e)
	}
	s, e := nas.OpenStore(database)
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.NamingEpoch(context.Background())
	s.Close()
	if e != nil || v != 0 {
		t.Fatal("draft activated")
	}
	if e := call("preview", "-id", "test", "-title", "Fixture [02]", "-source", "Fixture [02].mkv"); e != nil {
		t.Fatal(e)
	}
	if e := call("enable", "-id", "test"); e != nil {
		t.Fatal(e)
	}
	if e := call("disable", "-id", "test"); e != nil {
		t.Fatal(e)
	}
	lock, e := nas.AcquireInstance(database)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	if e = call("enable", "-id", "test"); e == nil {
		t.Fatal("daemon lock bypass")
	}
	if e = rulesCLI([]string{"parse", "-title", "[Group] Fiction 4th - 01~11 [总第67~77]"}); e != nil {
		t.Fatal(e)
	}
}
