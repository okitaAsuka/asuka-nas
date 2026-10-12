package nas

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigSafety(t *testing.T) {
	good := `{"database":"/data/state.db","downloads":"/media/downloads","library":"/media/library","seed_days":7,"qb_url":"http://qb:8080","emby_url":"http://emby:8096"}`
	c, e := LoadConfig(strings.NewReader(good))
	if e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(good, `"seed_days":7`, `"seed_days":8`, 1), strings.Replace(good, "http://qb:8080", "http://user:secret@qb:8080", 1), strings.Replace(good, "/media/library", "/media/downloads/sub", 1), good + ` {}`, strings.Replace(good, `"database"`, `"unknown"`, 1)} {
		if _, e = LoadConfig(strings.NewReader(bad)); e == nil {
			t.Fatal("accepted invalid config")
		}
	}
	var b bytes.Buffer
	slog.New(slog.NewJSONHandler(&b, nil)).Info("config", "config", c)
	if strings.Contains(b.String(), "http") || strings.Contains(b.String(), "/media") {
		t.Fatal("config leaked")
	}
}

func TestStoreAtomicReplay(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s, e := OpenStore(path)
	if e != nil {
		t.Fatal(e)
	}
	id, e := s.Ingest(ctx, "s", "guid", "hash", "secret", 0)
	if e != nil {
		t.Fatal(e)
	}
	other, e := s.Ingest(ctx, "s", "guid", "hash", "secret", 0)
	if e != nil || other != id {
		t.Fatal(other, e)
	}
	other, e = s.Ingest(ctx, "other", "guid2", "hash", "secret", 7)
	if e != nil || other != id {
		t.Fatal(other, e)
	}
	j, e := s.Get(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	stale := j
	j.Download = "requested"
	if e = s.Save(ctx, &j, "download_requested"); e != nil {
		t.Fatal(e)
	}
	if e = s.Save(ctx, &stale, "download_requested"); e == nil {
		t.Fatal("stale CAS accepted")
	}
	s.Close()
	s, e = OpenStore(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ev, e := s.Events(ctx, 0)
	if e != nil || len(ev) != 2 {
		t.Fatal(ev, e)
	}
	tail, e := s.Events(ctx, ev[0].ID)
	if e != nil || len(tail) != 1 {
		t.Fatal(tail, e)
	}
	j, e = s.Get(ctx, id)
	if e != nil || j.Download != "requested" || j.Version != 2 {
		t.Fatal(j, e)
	}
}

func TestRejectFutureSchema(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.db")
	s, e := OpenStore(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.Exec("PRAGMA user_version=3"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if s, e = OpenStore(p); e == nil {
		s.Close()
		t.Fatal("future schema overwritten")
	}
}

func TestSchemaMigrationAndReopenedConnectionPragmas(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.Ingest(ctx, "fixture", "guid", fixtureHash, "magnet:?xt=urn:btih:"+fixtureHash, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("DROP TABLE source_events; DROP TABLE source_state; DROP TABLE torrent_payloads; PRAGMA user_version=1;"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Get(ctx, id); err != nil {
		t.Fatal("migration lost job", err)
	}
	if ok, err := s.ClaimSource(ctx, "fixture", 1000, 60); err != nil || !ok {
		t.Fatal("migration missing source schema", err)
	}
	// 强制回收连接，验证连接级 PRAGMA 不依赖首次 OpenStore 的偶然连接。
	s.db.SetMaxIdleConns(0)
	for name, want := range map[string]int{"foreign_keys": 1, "synchronous": 2, "busy_timeout": 5000} {
		var got int
		if err = s.db.QueryRow("PRAGMA " + name).Scan(&got); err != nil || got != want {
			t.Fatal(name, got, err)
		}
	}
}
