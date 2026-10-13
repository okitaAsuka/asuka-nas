package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ReleaseContext struct {
	Subscription string
	GUID         string
	Title        string
}

// IngestRelease 来源和 job 同一事务写入；未保存标题的资源不能抢先执行。
func (s *Store) IngestRelease(ctx context.Context, sub, guid, hash, magnet string, payload []byte, days int, title string) (string, error) {
	if len(payload) > 0 {
		h, e := TorrentHash(payload)
		if e != nil || h != hash {
			return "", errors.New("torrent_invalid")
		}
	}
	return s.ingest(ctx, sub, guid, hash, magnet, payload, days, title)
}
func attachContext(ctx context.Context, tx *sql.Tx, id, sub, guid, title string) error {
	if len(title) > TitleLimit {
		return errors.New("title_too_large")
	}
	// 即使标题为空也保留来源关系；不能忽略一个无法映射的订阅。
	r, e := tx.ExecContext(ctx, "INSERT OR IGNORE INTO release_context VALUES(?,?,?,?,?)", id, sub, guid, digestString(title), title)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return nil
	}
	var body string
	if e = tx.QueryRowContext(ctx, "SELECT body FROM jobs WHERE id=?", id).Scan(&body); e != nil {
		return e
	}
	var j Job
	if e = json.Unmarshal([]byte(body), &j); e != nil {
		return e
	}
	j.ContextVersion++
	j.Version++
	j.NextAttempt = 0
	b, _ := json.Marshal(j)
	if _, e = tx.ExecContext(ctx, "UPDATE jobs SET body=?,version=? WHERE id=?", string(b), j.Version, id); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO events(job_id,kind,created_at) VALUES(?,?,?)", id, "source_context_added", time.Now().Unix())
	return e
}
func (s *Store) AttachResourceTitle(ctx context.Context, sub, guid, title string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id string
	if e = tx.QueryRowContext(ctx, "SELECT job_id FROM resources WHERE subscription=? AND guid=?", sub, guid).Scan(&id); e != nil {
		return e
	}
	if e = attachContext(ctx, tx, id, sub, guid, title); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Contexts(ctx context.Context, id string) ([]ReleaseContext, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT subscription,guid,title FROM release_context WHERE job_id=? ORDER BY subscription,guid,title_hash", id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []ReleaseContext
	for rows.Next() {
		var c ReleaseContext
		if e = rows.Scan(&c.Subscription, &c.GUID, &c.Title); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) NamingEpoch(ctx context.Context) (int64, error) {
	var v int64
	e := s.db.QueryRowContext(ctx, "SELECT value FROM naming_epoch WHERE id=1").Scan(&v)
	return v, e
}

// AddRule 只创建不可变草稿，不激活规则。相同 series ID 的 provider/order 不得改变。
func (s *Store) AddRule(ctx context.Context, r NamingRule) error {
	if r.IdentityNormalizationVersion == 0 {
		r.IdentityNormalizationVersion = identityNormalizationVersion
	}
	if e := r.Validate(); e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	returnErr := addRuleTx(ctx, tx, r)
	if returnErr != nil {
		return returnErr
	}
	return tx.Commit()
}

// addRuleTx 允许人工审核与规则落库共享一个事务，仍只创建不可变草稿。
func addRuleTx(ctx context.Context, tx *sql.Tx, r NamingRule) error {
	var e error
	if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO manual_series VALUES(?,?)", r.SeriesID, r.Order); e != nil {
		return e
	}
	var order string
	if e = tx.QueryRowContext(ctx, "SELECT episode_order FROM manual_series WHERE id=?", r.SeriesID).Scan(&order); e != nil {
		return e
	}
	if order != r.Order {
		return errors.New("series_order_conflict")
	}
	b, _ := json.Marshal(r)
	if _, e = tx.ExecContext(ctx, "INSERT INTO naming_rules VALUES(?,?,?)", r.ID, r.Revision, string(b)); e != nil {
		return errors.New("rule_revision_exists")
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO naming_events(rule_id,revision,kind,created_at) VALUES(?,?,?,?)", r.ID, r.Revision, "rule_drafted", time.Now().Unix()); e != nil {
		return e
	}
	return nil
}
func (s *Store) Rule(ctx context.Context, id string, revision int) (NamingRule, error) {
	var r NamingRule
	var b string
	e := s.db.QueryRowContext(ctx, "SELECT body FROM naming_rules WHERE id=? AND revision=?", id, revision).Scan(&b)
	if e != nil {
		return r, errors.New("rule_not_found")
	}
	e = json.Unmarshal([]byte(b), &r)
	return r, e
}
func (s *Store) SetRule(ctx context.Context, id string, revision int, enabled bool) error {
	if _, e := s.Rule(ctx, id, revision); e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = setRuleTx(ctx, tx, id, revision, enabled); e != nil {
		return e
	}
	return tx.Commit()
}

// setRuleTx 保持既有epoch/启用审计边界，供单事务人工批准复用。
func setRuleTx(ctx context.Context, tx *sql.Tx, id string, revision int, enabled bool) error {
	var e error
	var oldRevision, oldEnabled int
	e = tx.QueryRowContext(ctx, "SELECT revision,enabled FROM naming_active WHERE id=?", id).Scan(&oldRevision, &oldEnabled)
	on := 0
	if enabled {
		on = 1
	}
	if e == nil && oldRevision == revision && oldEnabled == on {
		return nil
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO naming_active VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,enabled=excluded.enabled", id, revision, on); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE naming_epoch SET value=value+1 WHERE id=1"); e != nil {
		return e
	}
	kind := "rule_disabled"
	if enabled {
		kind = "rule_enabled"
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO naming_events(rule_id,revision,kind,created_at) VALUES(?,?,?,?)", id, revision, kind, time.Now().Unix()); e != nil {
		return e
	}
	return nil
}

// ResolveNaming 多来源必须得到相同决定；命中停用规则或矛盾不能退回文件名猜测。
func (s *Store) ResolveNaming(ctx context.Context, id, source string) (*NamingDecision, []string, error) {
	return s.resolveNaming(ctx, id, source, nil)
}
func (s *Store) resolveNaming(ctx context.Context, id, source string, frozen []string) (*NamingDecision, []string, error) {
	contexts, e := s.Contexts(ctx, id)
	if e != nil {
		return nil, nil, e
	}
	type active struct {
		r  NamingRule
		on int
	}
	var rules []active
	if frozen != nil {
		seen := map[string]bool{}
		for _, ref := range frozen {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			id, rev, ok := strings.Cut(ref, "@")
			revision, err := strconv.Atoi(rev)
			if !ok || err != nil {
				return nil, nil, errors.New("frozen_rule_invalid")
			}
			r, err := s.Rule(ctx, id, revision)
			if err != nil {
				return nil, nil, err
			}
			rules = append(rules, active{r, 1})
		}
	} else {
		rows, err := s.db.QueryContext(ctx, "SELECT r.body,a.enabled FROM naming_rules r JOIN naming_active a ON r.id=a.id AND r.revision=a.revision ORDER BY r.id")
		if err != nil {
			return nil, nil, err
		}
		for rows.Next() {
			var b string
			var a active
			if err = rows.Scan(&b, &a.on); err != nil {
				rows.Close()
				return nil, nil, err
			}
			if err = json.Unmarshal([]byte(b), &a.r); err != nil {
				rows.Close()
				return nil, nil, err
			}
			rules = append(rules, a)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, err
		}
	}

	var result *NamingDecision
	var refs []string
	unmatched := false
	for _, c := range contexts {
		p, parseErr := ParseTitle(c.Title)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		f, fileErr := ParseTitle(strings.TrimSuffix(filepath.Base(source), filepath.Ext(source)))
		if fileErr != nil {
			return nil, nil, fileErr
		}

		matched := false
		for _, a := range rules {
			if a.r.Subscription == c.Subscription {
				if _, err := a.r.comparisonView(); err != nil {
					return nil, nil, err
				}
			}
			if !a.r.matches(c.Subscription, p) {
				continue
			}
			matched = true
			if a.on == 0 {
				return nil, nil, errors.New("rule_disabled")
			}
			d, e := a.r.Resolve(c.Title, source)
			if e != nil {
				return nil, nil, e
			}
			if result != nil && *result != d {
				return nil, nil, errors.New("mapping_conflict")
			}
			result = &d
			refs = append(refs, fmt.Sprintf("%s@%d", a.r.ID, a.r.Revision))
		}
		if !matched {
			for _, a := range rules {
				if a.r.knownIdentity(c.Subscription, p) {
					return nil, nil, errors.New("rule_scope_ambiguous")
				}
			}
			if p.Collection {
				return nil, nil, errors.New("collection_preview_only")
			}
			if p.Relative != nil && f.Relative != nil && *p.Relative != *f.Relative {
				return nil, nil, errors.New("title_file_conflict")
			}
			if p.Season != 0 && f.Season != 0 && p.Season != f.Season {
				return nil, nil, errors.New("season_conflict")
			}
			if p.Absolute != nil || f.Absolute != nil {
				return nil, nil, errors.New("absolute_mapping_unresolved")
			}
			unmatched = true
		}
	}
	if result != nil && unmatched {
		return nil, nil, errors.New("source_mapping_unresolved")
	}
	return result, refs, nil
}

// Replan 只撤销未开始的方案；旧方案以事件标记失效，历史文件不自动移动。
func (s *Store) Replan(ctx context.Context, id string) error {
	j, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	if j.Import == "imported" || (j.Plan != nil && j.Plan.Started) {
		return errors.New("plan_frozen")
	}
	j.Plan = nil
	j.Import = "pending"
	j.ErrorCode = ""
	j.BlockReason = ""
	j.NextAttempt = 0
	return s.Save(ctx, &j, "plan_invalidated")
}
