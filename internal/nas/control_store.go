package nas

import (
	"context"
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
 CREATE TABLE IF NOT EXISTS control_events(id INTEGER PRIMARY KEY AUTOINCREMENT,subject TEXT NOT NULL,kind TEXT NOT NULL,created_at INTEGER NOT NULL);`)
	if err != nil {
		return err
	}
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT version FROM control_meta").Scan(&version); err != nil {
		return err
	}
	if version != 1 {
		return errors.New("unsupported_control_schema")
	}
	for _, p := range profiles {
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO subscriptions VALUES(?)", p.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO managed_subscriptions VALUES(?,?,?,1,1)", p.ID, p.ID, p.ID); err != nil {
			return err
		}
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

func (s *Store) ManagedSubscriptions(ctx context.Context) ([]ManagedSubscription, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,profile,name,enabled,version FROM managed_subscriptions ORDER BY id LIMIT 101")
	if err != nil {
		return nil, err
	}
	out := []ManagedSubscription{}
	for rows.Next() {
		var v ManagedSubscription
		if err = rows.Scan(&v.ID, &v.Profile, &v.Name, &v.Enabled, &v.Version); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Status, err = s.SourceStatus(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
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
	if v.Version == 0 {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM managed_subscriptions").Scan(&count); err != nil {
			return err
		}
		if count >= 100 {
			return errors.New("subscription_limit")
		}
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
