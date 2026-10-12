package nas

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Job 的各状态正交，停止做种不能受导入失败影响。
type Job struct {
	NamingEpoch    int64    `json:"naming_epoch,omitempty"`
	ContextVersion int64    `json:"context_version,omitempty"`
	CheckedContext int64    `json:"checked_context,omitempty"`
	BlockReason    string   `json:"block_reason,omitempty"`
	HasTorrent     bool     `json:"has_torrent,omitempty"`
	Attempts       int      `json:"attempts,omitempty"`
	NextAttempt    int64    `json:"next_attempt,omitempty"`
	Sources        []string `json:"sources,omitempty"`
	ID             string   `json:"id"`
	Version        int      `json:"version"`
	Hash           string   `json:"hash"`
	Magnet         string   `json:"magnet"`
	Download       string   `json:"download"`
	Import         string   `json:"import"`
	Refresh        string   `json:"refresh"`
	Discovery      string   `json:"discovery"`
	Seed           string   `json:"seed"`
	CompletedAt    int64    `json:"completed_at"`
	StopAt         int64    `json:"stop_at"`
	SeedDays       int      `json:"seed_days"`
	ErrorCode      string   `json:"error_code"`
	Plan           *Plan    `json:"plan,omitempty"`
}

// Plan 在确认时冻结全部文件，版本或指纹变化即失效。
type Plan struct {
	ParserVersion  int             `json:"parser_version,omitempty"`
	Started        bool            `json:"started,omitempty"`
	RuleEpoch      int64           `json:"rule_epoch,omitempty"`
	ContextVersion int64           `json:"context_version,omitempty"`
	Decision       *NamingDecision `json:"decision,omitempty"`
	RuleRefs       []string        `json:"rule_refs,omitempty"`
	Version        int             `json:"version"`
	Approved       bool            `json:"approved"`
	Files          []PlannedFile   `json:"files"`
}

// PlannedFile 仅用相对路径，指纹覆盖内容与文件身份。
type PlannedFile struct {
	Source      string `json:"source"`
	Target      string `json:"target"`
	Fingerprint string `json:"fingerprint"`
}

// Event 不保存秘密上下文，可按 ID 至少一次重放。
type Event struct {
	ID            int64
	SchemaVersion int
	JobID         string
	Kind          string
}

// Store 只供本服务使用，外部 agent 不直接访问。
type Store struct{ db *sql.DB }

// OpenStore 初始化单机 WAL 数据库，不做生产迁移。
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, errors.New("database_directory")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, errors.New("database_open")
	}
	f.Close()
	if e = os.Chmod(path, 0600); e != nil {
		return nil, errors.New("database_permissions")
	}
	// 每个新连接都恢复安全 PRAGMA，连接因取消/错误重建时也不丢约束。
	dsn := url.URL{Scheme: "file", Path: path}
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(FULL)")
	dsn.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", dsn.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	var schema int
	if e = db.QueryRow("PRAGMA user_version").Scan(&schema); e != nil {
		db.Close()
		return nil, e
	}
	if schema > 3 {
		db.Close()
		return nil, errors.New("unsupported_schema_version")
	}

	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; BEGIN;
 CREATE TABLE IF NOT EXISTS subscriptions(id TEXT PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY, hash TEXT UNIQUE NOT NULL, version INTEGER NOT NULL, body TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS resources(subscription TEXT NOT NULL REFERENCES subscriptions(id), guid TEXT NOT NULL, job_id TEXT NOT NULL REFERENCES jobs(id), PRIMARY KEY(subscription,guid));
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT, schema_version INTEGER NOT NULL DEFAULT 1, job_id TEXT NOT NULL REFERENCES jobs(id), kind TEXT NOT NULL, created_at INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS torrent_payloads(hash TEXT PRIMARY KEY,data BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS source_state(id TEXT PRIMARY KEY REFERENCES subscriptions(id),next_poll INTEGER NOT NULL,failures INTEGER NOT NULL DEFAULT 0,error_code TEXT NOT NULL DEFAULT '');
 CREATE TABLE IF NOT EXISTS source_events(id INTEGER PRIMARY KEY AUTOINCREMENT,subscription TEXT NOT NULL REFERENCES subscriptions(id),kind TEXT NOT NULL,created_at INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS plan_history(job_id TEXT NOT NULL REFERENCES jobs(id),version INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(job_id,version));
 CREATE TABLE IF NOT EXISTS naming_rules(id TEXT NOT NULL,revision INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(id,revision));
 CREATE TABLE IF NOT EXISTS naming_active(id TEXT PRIMARY KEY,revision INTEGER NOT NULL,enabled INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS naming_epoch(id INTEGER PRIMARY KEY CHECK(id=1),value INTEGER NOT NULL);
 INSERT OR IGNORE INTO naming_epoch VALUES(1,0);
 CREATE TABLE IF NOT EXISTS manual_series(id TEXT PRIMARY KEY,episode_order TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS release_context(job_id TEXT NOT NULL REFERENCES jobs(id),subscription TEXT NOT NULL,guid TEXT NOT NULL,title_hash TEXT NOT NULL,title TEXT NOT NULL,PRIMARY KEY(subscription,guid,title_hash));
 CREATE TABLE IF NOT EXISTS naming_events(id INTEGER PRIMARY KEY AUTOINCREMENT,rule_id TEXT NOT NULL,revision INTEGER NOT NULL,kind TEXT NOT NULL,created_at INTEGER NOT NULL);
 PRAGMA user_version=3; COMMIT;`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Store{db: db}, nil
}

// Close 释放 SQLite 连接。
func (s *Store) Close() error { return s.db.Close() }

// Ingest 原子去重和追加事件，GUID 与 torrent hash 都有唯一约束。
func (s *Store) Ingest(ctx context.Context, subscription, guid, hash, magnet string, days int) (string, error) {
	return s.ingest(ctx, subscription, guid, hash, magnet, nil, days, "")
}

// IngestTorrent 原子保存完整 metainfo，重启后无需再次抓取秘密 enclosure URL。
func (s *Store) IngestTorrent(ctx context.Context, subscription, guid, hash string, payload []byte, days int) (string, error) {
	actual, err := TorrentHash(payload)
	if err != nil || actual != hash {
		return "", errors.New("torrent_invalid")
	}
	return s.ingest(ctx, subscription, guid, hash, "", payload, days, "")
}

func (s *Store) ingest(ctx context.Context, subscription, guid, hash, magnet string, payload []byte, days int, title string) (string, error) {
	if len(title) > TitleLimit {
		return "", errors.New("title_too_large")
	}
	if subscription == "" || guid == "" || days < 0 || days > 7 {
		return "", errors.New("invalid_resource")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var id string
	e = tx.QueryRowContext(ctx, "SELECT job_id FROM resources WHERE subscription=? AND guid=?", subscription, guid).Scan(&id)
	if e == nil {
		if e = attachContext(ctx, tx, id, subscription, guid, title); e != nil {
			return "", e
		}
		return id, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	sum := sha256.Sum256([]byte(hash))
	id = hex.EncodeToString(sum[:16])
	j := Job{ID: id, Version: 1, Hash: hash, Magnet: magnet, Download: "pending", Import: "pending", Refresh: "pending", Discovery: "pending", Seed: "pending", SeedDays: days, HasTorrent: len(payload) > 0}
	b, e := json.Marshal(j)
	if e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO subscriptions(id) VALUES(?)", subscription); e != nil {
		return "", e
	}
	r, e := tx.ExecContext(ctx, "INSERT OR IGNORE INTO jobs(id,hash,version,body) VALUES(?,?,?,?)", id, hash, 1, string(b))
	if e != nil {
		return "", e
	}
	n, _ := r.RowsAffected()
	if n == 1 && len(payload) > 0 {
		if _, e = tx.ExecContext(ctx, "INSERT INTO torrent_payloads(hash,data) VALUES(?,?)", hash, payload); e != nil {
			return "", e
		}
	}
	if n == 1 {
		if _, e = tx.ExecContext(ctx, "INSERT INTO events(job_id,kind,created_at) VALUES(?,?,?)", id, "resource_seen", time.Now().Unix()); e != nil {
			return "", e
		}
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO resources(subscription,guid,job_id) VALUES(?,?,?)", subscription, guid, id); e != nil {
		return "", e
	}
	if e = attachContext(ctx, tx, id, subscription, guid, title); e != nil {
		return "", e
	}
	return id, tx.Commit()
}

// Get 返回领域快照；调用方通过 Save 乐观并发更新。
func (s *Store) Get(ctx context.Context, id string) (Job, error) {
	var j Job
	var b string
	e := s.db.QueryRowContext(ctx, "SELECT body FROM jobs WHERE id=?", id).Scan(&b)
	if e != nil {
		return j, e
	}
	e = json.Unmarshal([]byte(b), &j)
	return j, e
}

// Save 用版本 CAS 保证领域状态和事件不可分离。
func (s *Store) Save(ctx context.Context, j *Job, kind string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if kind == "import_started" && j.Plan != nil {
		var epoch int64
		if e = tx.QueryRowContext(ctx, "SELECT value FROM naming_epoch WHERE id=1").Scan(&epoch); e != nil {
			return e
		}
		if j.Plan.RuleEpoch != epoch || j.Plan.ContextVersion != j.ContextVersion {
			return errors.New("stale_plan")
		}
	}
	if kind == "plan_ready" && j.Plan != nil {
		b, err := json.Marshal(j.Plan)
		if err != nil {
			return err
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO plan_history VALUES(?,?,?)", j.ID, j.Plan.Version, string(b)); e != nil {
			return e
		}
	}
	next := *j
	next.Version++
	b, e := json.Marshal(next)
	if e != nil {
		return e
	}
	r, e := tx.ExecContext(ctx, "UPDATE jobs SET body=?,version=? WHERE id=? AND version=?", string(b), next.Version, j.ID, j.Version)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return errors.New("stale_version")
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO events(job_id,kind,created_at) VALUES(?,?,?)", j.ID, kind, time.Now().Unix()); e != nil {
		return e
	}
	if e = tx.Commit(); e == nil {
		*j = next
	}
	return e
}

// IDs 只返回不透明 ID，不暴露 RSS 或认证字段。
func (s *Store) IDs(ctx context.Context) ([]string, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT id FROM jobs ORDER BY id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Events 按游标读取有界事件页，失败不推进消费者游标。
func (s *Store) Events(ctx context.Context, after int64) ([]Event, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT id,schema_version,job_id,kind FROM events WHERE id>? ORDER BY id LIMIT 100", after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var result []Event
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.ID, &v.SchemaVersion, &v.JobID, &v.Kind); e != nil {
			return nil, e
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// Torrent 返回已验证的元数据；不会返回或重新抓取订阅 URL。
func (s *Store) Torrent(ctx context.Context, hash string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, "SELECT data FROM torrent_payloads WHERE hash=?", hash).Scan(&b)
	return b, err
}

// ResourceSeen 避免重复 feed 项再次下载 metainfo。
func (s *Store) ResourceSeen(ctx context.Context, subscription, guid string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM resources WHERE subscription=? AND guid=?", subscription, guid).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func digestString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
