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
	if _, e = s.db.Exec("PRAGMA user_version=2"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if s, e = OpenStore(p); e == nil {
		s.Close()
		t.Fatal("future schema overwritten")
	}
}
