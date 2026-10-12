package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type legacyEvidence struct {
	Manual   *ManualSemantics  `json:"manual"`
	Evidence []ContextEvidence `json:"evidence"`
}

// 迁移与运行时共用预约检查，包括目标的文件/目录前缀冲突；全部在一个事务中。
func migratePlansTx(ctx context.Context, tx *sql.Tx) error {
	rows, e := tx.QueryContext(ctx, "SELECT body FROM jobs ORDER BY id")
	if e != nil {
		return e
	}
	var jobs []Job
	for rows.Next() {
		var b string
		var j Job
		if e = rows.Scan(&b); e != nil {
			rows.Close()
			return e
		}
		if e = json.Unmarshal([]byte(b), &j); e != nil {
			rows.Close()
			return e
		}
		jobs = append(jobs, j)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, j := range jobs {
		if j.Plan == nil {
			continue
		}
		if e = reservePlanTx(ctx, tx, &j); e != nil {
			return e
		}
		b, _ := json.Marshal(j.Plan)
		if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO plan_history VALUES(?,?,?)", j.ID, j.Plan.Version, string(b)); e != nil {
			return e
		}
		if j.Plan.Manual != nil || len(j.Plan.Evidence) > 0 || len(j.Plan.RuleRefs) > 0 {
			continue
		}
		proof := legacyEvidence{Manual: targetSemantics(j.Plan.Files[0].Target)}
		proven := proof.Manual != nil && j.Plan.ContextVersion == j.ContextVersion && !j.Plan.Collection
		contexts, e := tx.QueryContext(ctx, "SELECT subscription,title FROM release_context WHERE job_id=?", j.ID)
		if e != nil {
			return e
		}
		for contexts.Next() {
			var sub, title string
			if e = contexts.Scan(&sub, &title); e != nil {
				contexts.Close()
				return e
			}
			if checkManualEvidence(proof.Manual, title) != nil {
				proven = false
			}
			proof.Evidence = append(proof.Evidence, ContextEvidence{sub, digestString(title)})
		}
		e = contexts.Err()
		contexts.Close()
		if e != nil {
			return e
		}
		if proven && len(proof.Evidence) > 0 {
			b, _ := json.Marshal(proof)
			if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO legacy_plan_evidence VALUES(?,?,?)", j.ID, j.Plan.Version, string(b)); e != nil {
				return e
			}
		} else {
			j.BlockReason = "legacy_evidence_unresolved"
			j.ErrorCode = j.BlockReason
			j.Version++
			b, _ := json.Marshal(j)
			if _, e = tx.ExecContext(ctx, "UPDATE jobs SET body=?,version=? WHERE id=?", string(b), j.Version, j.ID); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, "INSERT INTO events(job_id,kind,created_at,plan_version) VALUES(?, 'legacy_evidence_unresolved',?,?)", j.ID, time.Now().Unix(), j.Plan.Version); e != nil {
				return e
			}
		}
	}
	return nil
}
