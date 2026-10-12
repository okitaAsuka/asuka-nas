package nas

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

// Job 的各状态正交，停止做种不能受导入失败影响。
type Job struct {
	Sources     []string `json:"sources,omitempty"`
	ID          string   `json:"id"`
	Version     int      `json:"version"`
	Hash        string   `json:"hash"`
	Magnet      string   `json:"magnet"`
	Download    string   `json:"download"`
	Import      string   `json:"import"`
	Refresh     string   `json:"refresh"`
	Discovery   string   `json:"discovery"`
	Seed        string   `json:"seed"`
	CompletedAt int64    `json:"completed_at"`
	StopAt      int64    `json:"stop_at"`
	SeedDays    int      `json:"seed_days"`
	ErrorCode   string   `json:"error_code"`
	Plan        *Plan    `json:"plan,omitempty"`
}

// Plan 在确认时冻结全部文件，版本或指纹变化即失效。
type Plan struct {
	Version  int           `json:"version"`
	Approved bool          `json:"approved"`
	Files    []PlannedFile `json:"files"`
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
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	var schema int
	if e = db.QueryRow("PRAGMA user_version").Scan(&schema); e != nil {
		db.Close()
		return nil, e
	}
	if schema > 1 {
		db.Close()
		return nil, errors.New("unsupported_schema_version")
	}

	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS subscriptions(id TEXT PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS jobs(id TEXT PRIMARY KEY, hash TEXT UNIQUE NOT NULL, version INTEGER NOT NULL, body TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS resources(subscription TEXT NOT NULL REFERENCES subscriptions(id), guid TEXT NOT NULL, job_id TEXT NOT NULL REFERENCES jobs(id), PRIMARY KEY(subscription,guid));
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT, schema_version INTEGER NOT NULL DEFAULT 1, job_id TEXT NOT NULL REFERENCES jobs(id), kind TEXT NOT NULL, created_at INTEGER NOT NULL);
 PRAGMA user_version=1;`)
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
		return id, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	sum := sha256.Sum256([]byte(hash))
	id = hex.EncodeToString(sum[:16])
	j := Job{ID: id, Version: 1, Hash: hash, Magnet: magnet, Download: "pending", Import: "pending", Refresh: "pending", Discovery: "pending", Seed: "pending", SeedDays: days}
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
	if n == 1 {
		if _, e = tx.ExecContext(ctx, "INSERT INTO events(job_id,kind,created_at) VALUES(?,?,?)", id, "resource_seen", time.Now().Unix()); e != nil {
			return "", e
		}
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO resources(subscription,guid,job_id) VALUES(?,?,?)", subscription, guid, id); e != nil {
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
