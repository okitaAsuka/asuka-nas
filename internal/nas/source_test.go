package nas

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const validTorrent = `d4:infod6:lengthi4e4:name15:Show.S01E01.mkv12:piece lengthi4e6:pieces20:aaaaaaaaaaaaaaaaaaaaee`

func TestTorrentExactInfoHashAndRejectMalformed(t *testing.T) {
	hash, err := TorrentHash([]byte(validTorrent))
	if err != nil || hash != "bfe743a7dce0ad599e0c5624b9382f834a8d245b" {
		t.Fatal(hash, err)
	}
	// 字典外的注释改变不应改变 infohash。
	commented := strings.Replace(validTorrent, "d4:info", "d7:comment6:secret4:info", 1)
	if other, err := TorrentHash([]byte(commented)); err != nil || other != hash {
		t.Fatal(other, err)
	}
	invalid := []string{validTorrent + "x", strings.Replace(validTorrent, "i4e", "i04e", 1), strings.Replace(validTorrent, "i4e", "i-0e", 1), strings.Replace(validTorrent, "15:Show.S01E01.mkv", "3:../", 1), strings.Replace(validTorrent, "20:aaaaaaaaaaaaaaaaaaaa", "19:aaaaaaaaaaaaaaaaaaa", 1), "d4:info" + strings.Repeat("l", 34) + strings.Repeat("e", 35), "d4:infoi1e4:infoi2ee"}
	for _, b := range invalid {
		if _, err := TorrentHash([]byte(b)); err == nil {
			t.Fatal("malformed torrent accepted")
		}
	}
}

func TestRSSHTTPToTorrentUploadAndRestart(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", -1)
	f.hash, _ = TorrentHash([]byte(validTorrent))
	var feedCalls, torrentCalls atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rss":
			feedCalls.Add(1)
			fmt.Fprint(w, `<rss><channel><item><guid>private-guid</guid><title>&lt;img onerror='bad'&gt;</title><enclosure url="/episode.torrent?token=private"/></item></channel></rss>`)
		case "/episode.torrent":
			torrentCalls.Add(1)
			fmt.Fprint(w, validTorrent)
		default:
			t.Error("unexpected source URL")
		}
	}))
	defer source.Close()
	cfg := Config{Database: f.db, Downloads: f.downloads, Library: f.library, QBURL: f.server.URL, EmbyURL: f.server.URL, SeedDays: 0, PollSeconds: 60, Subscriptions: []Subscription{{ID: "mikan-fixture", URL: source.URL + "/rss?token=private"}}}
	cfg.defaults()
	d := &Daemon{Config: cfg, Engine: f.engine, Source: NewSourceClient(time.Second, 2048, 2048, 10)}
	ctx := context.Background()
	if err := d.PollSources(ctx, time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	f.reopen()
	d.Engine = f.engine
	if err := d.PollSources(ctx, time.Unix(1001, 0)); err != nil {
		t.Fatal(err)
	}
	if feedCalls.Load() != 1 || torrentCalls.Load() != 1 {
		t.Fatal("restart bypassed reservation")
	}
	f.tick(1001)
	j := f.job()
	if !j.HasTorrent || j.Import != "imported" || j.Seed != "stopped" || j.Discovery != "pending" {
		t.Fatalf("bad torrent loop: %+v", j)
	}
	if err := d.PollSources(ctx, time.Unix(1060, 0)); err != nil {
		t.Fatal(err)
	}
	if feedCalls.Load() != 2 || torrentCalls.Load() != 1 {
		t.Fatal("duplicate enclosure fetched")
	}
	f.mu.Lock()
	count := f.multipartAdds
	f.mu.Unlock()
	if count != 1 {
		t.Fatal("torrent not uploaded")
	}
	events, _ := f.store.Events(ctx, 0)
	for _, event := range events {
		if strings.Contains(fmt.Sprint(event), "private") {
			t.Fatal("secret in event")
		}
	}
	// 明文 URL 不进入 job/resource；完整 metainfo 的 tracker 信息只在权限受控 blob 内。
	var body string
	if err := f.store.db.QueryRow("SELECT body FROM jobs").Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "private") {
		t.Fatal("secret URL in job")
	}
}

func TestSourceFailureBackoffPersists(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", -1)
	var calls atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(503)
		fmt.Fprint(w, "token=private")
	}))
	defer source.Close()
	d := &Daemon{Config: Config{PollSeconds: 60, Subscriptions: []Subscription{{ID: "test", URL: source.URL + "?token=private"}}}, Engine: f.engine, Source: NewSourceClient(time.Second, 2048, 2048, 10)}
	ctx := context.Background()
	if err := d.PollSources(ctx, time.Unix(1000, 0)); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	f.reopen()
	d.Engine = f.engine
	if err := d.PollSources(ctx, time.Unix(1059, 0)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("polled too soon")
	}
	if err := d.PollSources(ctx, time.Unix(1060, 0)); err == nil {
		t.Fatal("expected failure")
	}
	state, err := f.store.SourceStatus(ctx, "test")
	if err != nil || state.Failures != 2 || state.NextPoll != 1180 {
		t.Fatal(state, err)
	}
	if err := d.PollSources(ctx, time.Unix(1179, 0)); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("backoff not persisted")
	}
}

func TestEnclosureOriginRedirectAndBodyLimits(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", -1)
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1); fmt.Fprint(w, validTorrent) }))
	defer target.Close()
	mode := "origin"
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "origin":
			fmt.Fprintf(w, `<rss><channel><item><guid>x</guid><enclosure url="%s/a?token=private"/></item></channel></rss>`, target.URL)
		case "redirect":
			http.Redirect(w, r, target.URL+"?token=private", 302)
		case "large":
			fmt.Fprint(w, strings.Repeat("x", 1025))
		case "timeout":
			<-r.Context().Done()
		}
	}))
	defer source.Close()
	client := NewSourceClient(30*time.Millisecond, 1024, 1024, 10)
	sub := Subscription{ID: "test", URL: source.URL + "?token=private"}
	for _, m := range []string{"origin", "redirect", "large", "timeout"} {
		mode = m
		err := client.Poll(context.Background(), sub, f.store, 0)
		if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), source.URL) {
			t.Fatal(m, err)
		}
	}
	if targetCalls.Load() != 0 {
		t.Fatal("cross-origin/redirect request escaped")
	}
}

func TestDaemonCancellationAndInstanceLock(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", -1)
	lock, err := AcquireInstance(f.db)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := AcquireInstance(f.db); err == nil {
		second.Close()
		t.Fatal("duplicate daemon accepted")
	}
	lock.Close()
	started := make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer source.Close()
	cfg := Config{Database: f.db, Downloads: f.downloads, Library: f.library, QBURL: f.server.URL, EmbyURL: f.server.URL, Subscriptions: []Subscription{{ID: "cancel", URL: source.URL + "?token=private"}}, TickSeconds: 1}
	cfg.defaults()
	var logs bytes.Buffer
	d := &Daemon{Config: cfg, Engine: f.engine, Source: NewSourceClient(time.Minute, 1024, 1024, 10), Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("daemon did not poll")
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked by HTTP")
	}
	if strings.Contains(logs.String(), "private") || strings.Contains(logs.String(), source.URL) {
		t.Fatal("log leak")
	}
	if _, err = os.Stat(filepath.Join(f.downloads, f.name)); err != nil {
		t.Fatal("shutdown removed source")
	}
}

func TestJobRetrySurvivesRestartWithoutDelayingStop(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", 7)
	f.mu.Lock()
	f.failRefresh = true
	f.mu.Unlock()
	ctx := context.Background()
	if err := f.engine.RetryTick(ctx, time.Unix(1000, 0), 30); err == nil {
		t.Fatal("expected refresh failure")
	}
	f.reopen()
	if err := f.engine.RetryTick(ctx, time.Unix(1010, 0), 30); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	calls := f.refreshes
	f.mu.Unlock()
	if calls != 1 {
		t.Fatal("retry ignored deadline")
	}
	j := f.job()
	j.NextAttempt = j.StopAt + 100
	if err := f.store.Save(ctx, &j, "retry_scheduled"); err != nil {
		t.Fatal(err)
	}
	if err := f.engine.RetryTick(ctx, time.Unix(j.StopAt, 0), 30); err != nil {
		t.Fatal(err)
	}
	if f.job().Seed != "stopped" {
		t.Fatal("import retry postponed seed deadline")
	}
}

func FuzzTorrentHashDoesNotPanic(f *testing.F) {
	f.Add([]byte(validTorrent))
	f.Add([]byte("de"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 4096 {
			return
		}
		_, _ = TorrentHash(b)
	})
}

type channelLog struct{ lines chan string }

func (w channelLog) Write(b []byte) (int, error) { w.lines <- string(b); return len(b), nil }

func TestDaemonFailureLogsOnlySafeCodes(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", -1)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500); fmt.Fprint(w, "fixture-secret") }))
	defer source.Close()
	cfg := Config{Database: f.db, Downloads: f.downloads, Library: f.library, QBURL: f.server.URL, EmbyURL: f.server.URL, Subscriptions: []Subscription{{ID: "failure", URL: source.URL + "?token=fixture-secret"}}}
	cfg.defaults()
	lines := make(chan string, 4)
	d := &Daemon{Config: cfg, Engine: f.engine, Source: NewSourceClient(time.Second, 1024, 1024, 10), Logger: slog.New(slog.NewJSONHandler(channelLog{lines}, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	select {
	case line := <-lines:
		if !strings.Contains(line, "source_cycle_failed") || strings.Contains(line, "fixture-secret") || strings.Contains(line, source.URL) {
			t.Fatal("unsafe log", line)
		}
	case <-time.After(time.Second):
		t.Fatal("missing failure log")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	events, err := f.store.SourceEvents(context.Background(), 0)
	if err != nil || len(events) != 1 || events[0].Kind != "poll_failed" {
		t.Fatal(events, err)
	}
}

func TestSourceReservationSurvivesCrashBeforeRequest(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", -1)
	ctx := context.Background()
	if ok, err := f.store.ClaimSource(ctx, "test", 1000, 60); err != nil || !ok {
		t.Fatal(ok, err)
	}
	f.reopen()
	if ok, err := f.store.ClaimSource(ctx, "test", 1001, 60); err != nil || ok {
		t.Fatal("reservation lost", ok, err)
	}
	if ok, err := f.store.ClaimSource(ctx, "test", 1060, 60); err != nil || !ok {
		t.Fatal("reservation never expires", ok, err)
	}
}

func encodeFixture(v any) string {
	switch v := v.(type) {
	case string:
		return fmt.Sprintf("%d:%s", len(v), v)
	case int:
		return fmt.Sprintf("i%de", v)
	case []any:
		s := "l"
		for _, x := range v {
			s += encodeFixture(x)
		}
		return s + "e"
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		s := "d"
		for _, key := range keys {
			s += encodeFixture(key) + encodeFixture(v[key])
		}
		return s + "e"
	default:
		panic("unsupported fixture")
	}
}

func TestMultiFileTorrentRejectsPathAndAliasEscapes(t *testing.T) {
	makeInfo := func() map[string]any {
		return map[string]any{"name": "Show", "piece length": 5, "pieces": strings.Repeat("a", 20), "files": []any{map[string]any{"length": 4, "path": []any{"Show.S01E01.mkv"}}, map[string]any{"length": 1, "path": []any{"Show.S01E01.ass"}}}}
	}
	if _, err := TorrentHash([]byte(encodeFixture(map[string]any{"info": makeInfo()}))); err != nil {
		t.Fatal("valid multi-file rejected", err)
	}
	for _, mutate := range []func(map[string]any){
		func(i map[string]any) { i["files"].([]any)[0].(map[string]any)["path"] = []any{"..", "escape.mkv"} },
		func(i map[string]any) {
			i["files"].([]any)[0].(map[string]any)["path.utf-8"] = []any{"..", "escape.mkv"}
		},
		func(i map[string]any) { i["name.utf-8"] = "../outside" },
		func(i map[string]any) { i["files"].([]any)[0].(map[string]any)["attr"] = "l" },
		func(i map[string]any) { i["files"].([]any)[0].(map[string]any)["symlink path"] = []any{"outside"} },
		func(i map[string]any) { i["files"].([]any)[1].(map[string]any)["path"] = []any{"Show.S01E01.mkv"} },
	} {
		info := makeInfo()
		mutate(info)
		if _, err := TorrentHash([]byte(encodeFixture(map[string]any{"info": info}))); err == nil {
			t.Fatal("unsafe metainfo accepted")
		}
	}
}

func TestPersistedTorrentCorruptionCannotDispatchDifferentHash(t *testing.T) {
	f := newFixture(t, "Show.S01E01.mkv", -1)
	hash, err := TorrentHash([]byte(validTorrent))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.IngestTorrent(context.Background(), "test", "guid", hash, []byte(validTorrent), 0); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(validTorrent, "Show.S01E01.mkv", "Show.S02E01.mkv", 1)
	if _, err = f.store.db.Exec("UPDATE torrent_payloads SET data=?", []byte(changed)); err != nil {
		t.Fatal(err)
	}
	if err = f.engine.Tick(context.Background(), time.Unix(1000, 0)); err == nil {
		t.Fatal("corrupt payload accepted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.adds != 0 {
		t.Fatal("wrong torrent dispatched")
	}
}
