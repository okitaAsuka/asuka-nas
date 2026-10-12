package nas

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SourceState 是重启可恢复的限频和错误事实，不包含原始 URL。
type SourceState struct {
	NextPoll  int64
	Failures  int
	ErrorCode string
}

// ClaimSource 在发请求前预约下次时间；崩溃不能绕过轮询间隔。
func (s *Store) ClaimSource(ctx context.Context, id string, now int64, interval int) (bool, error) {
	// 未到期只读，空闲 tick 不反复开启写事务；事务内仍以条件 UPDATE 仲裁。
	var next int64
	err := s.db.QueryRowContext(ctx, "SELECT next_poll FROM source_state WHERE id=?", id).Scan(&next)
	if err == nil && next > now {
		return false, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO subscriptions(id) VALUES(?)", id); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO source_state(id,next_poll) VALUES(?,0)", id); err != nil {
		return false, err
	}
	r, err := tx.ExecContext(ctx, "UPDATE source_state SET next_poll=? WHERE id=? AND next_poll<=?", now+int64(interval), id, now)
	if err != nil {
		return false, err
	}
	count, err := r.RowsAffected()
	if err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return count == 1, nil
}

// SourceStatus 用于 CLI 与本地运维查询，不返回 token。
func (s *Store) SourceStatus(ctx context.Context, id string) (SourceState, error) {
	var v SourceState
	err := s.db.QueryRowContext(ctx, "SELECT next_poll,failures,error_code FROM source_state WHERE id=?", id).Scan(&v.NextPoll, &v.Failures, &v.ErrorCode)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	return v, err
}

// FinishSource 状态和轮询事件一起落盘，错误只保留固定码。
func (s *Store) FinishSource(ctx context.Context, id string, now int64, interval int, failed bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	failures := 0
	code := ""
	kind := "poll_succeeded"
	if failed {
		if err = tx.QueryRowContext(ctx, "SELECT failures FROM source_state WHERE id=?", id).Scan(&failures); err != nil {
			return err
		}
		if failures < 10 {
			failures++
		}
		code = "source_poll_failed"
		kind = "poll_failed"
	}
	delay := int64(interval)
	if failed {
		delay = retryDelay(interval, failures)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE source_state SET next_poll=?,failures=?,error_code=? WHERE id=?", now+delay, failures, code, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO source_events(subscription,kind,created_at) VALUES(?,?,?)", id, kind, now); err != nil {
		return err
	}
	return tx.Commit()
}

func retryDelay(base, failures int) int64 {
	delay := int64(base)
	for i := 1; i < failures && delay < 3600; i++ {
		delay *= 2
	}
	if delay > 3600 && base < 3600 {
		return 3600
	}
	if delay > int64(base) && base >= 3600 {
		return int64(base)
	}
	return delay
}

// RetryTick 的下载/导入退避持久化；已知做种截止检查始终绕过退避。
func (e *Engine) RetryTick(ctx context.Context, now time.Time, base int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	ids, err := e.Store.IDs(ctx)
	if err != nil {
		return err
	}
	blocked := map[string]bool{}
	var failures []error
	for _, id := range ids {
		j, err := e.Store.Get(ctx, id)
		if err != nil {
			return err
		}
		if err = e.stop(ctx, &j, now); err != nil {
			blocked[id] = true
			failures = append(failures, err)
		}
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if blocked[id] {
			continue
		}
		j, err := e.Store.Get(ctx, id)
		if err != nil {
			return err
		}
		if j.NextAttempt > now.Unix() {
			continue
		}
		// 发请求前先预约，崩溃重启不会跳过最小尝试间隔。
		if j.Download == "completed" && (j.Import == "needs_confirmation" || (j.Import == "imported" && j.Discovery == "confirmed")) {
			continue
		}
		j.NextAttempt = now.Unix() + int64(base)
		if err = e.Store.Save(ctx, &j, "attempt_started"); err != nil {
			return err
		}
		runErr := e.tick(ctx, id, now)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		j, err = e.Store.Get(ctx, id)
		if err != nil {
			return err
		}
		if runErr != nil {
			if j.Attempts < 10 {
				j.Attempts++
			}
			j.NextAttempt = now.Unix() + retryDelay(base, j.Attempts)
			if err = e.Store.Save(ctx, &j, "retry_scheduled"); err != nil {
				return err
			}
			failures = append(failures, runErr)
		} else if j.Attempts > 0 {
			j.Attempts = 0
			if j.Import != "needs_confirmation" {
				j.ErrorCode = ""
			}
			if err = e.Store.Save(ctx, &j, "retry_cleared"); err != nil {
				return err
			}
		}
	}
	return errors.Join(failures...)
}

// SourceEvent 与 job 事件使用独立游标空间，不携带 RSS URL 或错误原文。
type SourceEvent struct {
	ID                 int64
	Subscription, Kind string
	CreatedAt          int64
}

// SourceEvents 提供可重放的有界来源事件页。
func (s *Store) SourceEvents(ctx context.Context, after int64) ([]SourceEvent, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,subscription,kind,created_at FROM source_events WHERE id>? ORDER BY id LIMIT 100", after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []SourceEvent
	for rows.Next() {
		var e SourceEvent
		if err = rows.Scan(&e.ID, &e.Subscription, &e.Kind, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
