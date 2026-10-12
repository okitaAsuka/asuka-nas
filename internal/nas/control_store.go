package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// 管理扩展有独立版本；不重写领域 schema v5 或历史计划。
func (s *Store) InitControl(ctx context.Context, profiles []Subscription) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS control_meta(version INTEGER NOT NULL); INSERT INTO control_meta SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM control_meta);
 CREATE TABLE IF NOT EXISTS managed_subscriptions(id TEXT PRIMARY KEY REFERENCES subscriptions(id),profile TEXT NOT NULL,name TEXT NOT NULL,enabled INTEGER NOT NULL,version INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS control_profiles(id TEXT PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS control_events(id INTEGER PRIMARY KEY AUTOINCREMENT,subject TEXT NOT NULL,kind TEXT NOT NULL,created_at INTEGER NOT NULL);`)
	if err != nil {
		return err
	}
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT version FROM control_meta").Scan(&version); err != nil {
		return err
	}
	if version != 1 && version != 2 {
		return errors.New("unsupported_control_schema")
	}
	// 当前配置档案与历史订阅分离；换配置不删除资源或用户启停状态。
	if len(profiles) > 100 {
		return errors.New("subscription_limit")
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM control_profiles"); err != nil {
		return err
	}
	for _, p := range profiles {
		if _, err = tx.ExecContext(ctx, "INSERT INTO control_profiles VALUES(?)", p.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO subscriptions VALUES(?)", p.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO managed_subscriptions VALUES(?,?,?,1,1)", p.ID, p.ID, p.ID); err != nil {
			return err
		}
	}
	if err = checkSubscriptionCapacity(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE control_meta SET version=2"); err != nil {
		return err
	}
	return tx.Commit()
}

type ManagedSubscription struct {
	ID        string      `json:"id"`
	Profile   string      `json:"profile"`
	Name      string      `json:"name"`
	Enabled   bool        `json:"enabled"`
	Version   int         `json:"version"`
	Status    SourceState `json:"status"`
	Available bool        `json:"available"`
}

// ManagedSubscriptions 完整历史查询；不得把 UI 页面当成调度全集。
func (s *Store) ManagedSubscriptions(ctx context.Context) ([]ManagedSubscription, error) {
	return s.managedSubscriptions(ctx, "ORDER BY m.id")
}
func (s *Store) ScheduledSubscriptions(ctx context.Context) ([]ManagedSubscription, error) {
	return s.managedSubscriptions(ctx, "WHERE m.enabled=1 AND EXISTS(SELECT 1 FROM control_profiles p WHERE p.id=m.profile) ORDER BY m.id")
}
func (s *Store) SubscriptionPage(ctx context.Context, after string) ([]ManagedSubscription, string, int, error) {
	items, err := s.managedSubscriptions(ctx, "WHERE m.id>? ORDER BY m.id LIMIT 101", after)
	if err != nil {
		return nil, "", 0, err
	}
	next := ""
	if len(items) > 100 {
		items = items[:100]
		next = items[99].ID
	}
	var total int
	err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM managed_subscriptions").Scan(&total)
	return items, next, total, err
}
func (s *Store) managedSubscriptions(ctx context.Context, suffix string, args ...any) ([]ManagedSubscription, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT m.id,m.profile,m.name,m.enabled,m.version,coalesce(s.next_poll,0),coalesce(s.failures,0),coalesce(s.error_code,'') FROM managed_subscriptions m LEFT JOIN source_state s ON s.id=m.id "+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManagedSubscription{}
	for rows.Next() {
		var v ManagedSubscription
		if err = rows.Scan(&v.ID, &v.Profile, &v.Name, &v.Enabled, &v.Version, &v.Status.NextPoll, &v.Status.Failures, &v.Status.ErrorCode); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// 统一限制当前可调度的启用订阅，不让历史/停用项占满活跃容量。
// 历史单独有限保存，达限整笔拒绝；不删 resources 或静默截断。
func checkSubscriptionCapacity(ctx context.Context, tx *sql.Tx) error {
	var active, total int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM managed_subscriptions WHERE enabled=1 AND profile IN (SELECT id FROM control_profiles)").Scan(&active); err != nil {
		return err
	}
	if active > 100 {
		return errors.New("subscription_limit")
	}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM managed_subscriptions").Scan(&total); err != nil {
		return err
	}
	if total > 10000 {
		return errors.New("subscription_history_limit")
	}
	return nil
}
func (s *Store) PutSubscription(ctx context.Context, v ManagedSubscription) error {
	if !subscriptionID.MatchString(v.ID) || !subscriptionID.MatchString(v.Profile) || len(v.Name) < 1 || len(v.Name) > 120 || v.Version < 0 {
		return errors.New("invalid_subscription")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var available bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM control_profiles WHERE id=?)", v.Profile).Scan(&available); err != nil {
		return err
	}
	if !available {
		if v.Version == 0 || v.Enabled {
			return errors.New("profile_unavailable")
		}
		var existing string
		if err = tx.QueryRowContext(ctx, "SELECT profile FROM managed_subscriptions WHERE id=?", v.ID).Scan(&existing); err != nil || existing != v.Profile {
			return errors.New("profile_unavailable")
		}
	}
	if v.Version == 0 {
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO subscriptions VALUES(?)", v.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO managed_subscriptions VALUES(?,?,?,?,1)", v.ID, v.Profile, v.Name, v.Enabled)
		if err != nil {
			return errors.New("subscription_exists")
		}
	} else {
		var profile string
		if err = tx.QueryRowContext(ctx, "SELECT profile FROM managed_subscriptions WHERE id=?", v.ID).Scan(&profile); err != nil {
			return errors.New("subscription_not_found")
		}
		if profile != v.Profile {
			var count int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM resources WHERE subscription=?", v.ID).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				return errors.New("subscription_frozen")
			}
		}
		r, e := tx.ExecContext(ctx, "UPDATE managed_subscriptions SET profile=?,name=?,enabled=?,version=version+1 WHERE id=? AND version=?", v.Profile, v.Name, v.Enabled, v.ID, v.Version)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return errors.New("stale_version")
		}
	}
	if err = checkSubscriptionCapacity(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO control_events(subject,kind,created_at) VALUES(?,?,?)", v.ID, "subscription_updated", time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

type RuleView struct {
	Rule    NamingRule `json:"rule"`
	Active  bool       `json:"active"`
	Enabled bool       `json:"enabled"`
}

func (s *Store) RuleViews(ctx context.Context) ([]RuleView, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT r.body,coalesce(a.revision=r.revision,0),coalesce(a.revision=r.revision AND a.enabled=1,0) FROM naming_rules r LEFT JOIN naming_active a ON a.id=r.id ORDER BY r.id,r.revision DESC LIMIT 500")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RuleView{}
	for rows.Next() {
		var v RuleView
		var body string
		if err = rows.Scan(&body, &v.Active, &v.Enabled); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(body), &v.Rule); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type AuditEvent struct {
	ID            int64
	Subject, Kind string
	Revision      int
	CreatedAt     int64
}

func (s *Store) Audit(ctx context.Context) ([]AuditEvent, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,rule_id,revision,kind,created_at FROM naming_events ORDER BY id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var v AuditEvent
		if err = rows.Scan(&v.ID, &v.Subject, &v.Revision, &v.Kind, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// RetrySource 与操作版本、审计同一事务，双击/重放不会反复解除轮询退避。
func (s *Store) RetrySource(ctx context.Context, id string, version int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, "UPDATE managed_subscriptions SET version=version+1 WHERE id=? AND version=? AND enabled=1", id, version)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return errors.New("stale_version")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE source_state SET next_poll=0 WHERE id=?", id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO control_events(subject,kind,created_at) VALUES(?,?,?)", id, "source_retry_requested", time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}
