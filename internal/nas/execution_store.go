package nas

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type FileExecution struct {
	Index      int
	State      string
	Discovered bool
	Video      bool
}

func videoTarget(target string) bool {
	v := strings.ToLower(target)
	return strings.HasSuffix(v, ".mkv") || strings.HasSuffix(v, ".mp4")
}

// reservePlanTx 在任何文件效果之前预约整包目标；同 inode 也不能被另一 job 偷占。
func reservePlanTx(ctx context.Context, tx *sql.Tx, j *Job) error {
	if j.Plan == nil || len(j.Plan.Files) == 0 || len(j.Plan.Files) > 512 {
		return errors.New("invalid_plan")
	}
	seen := map[string]bool{}
	for i, f := range j.Plan.Files {
		if !local(f.Source) || !local(f.Target) || seen[f.Target] {
			return errors.New("target_conflict")
		}
		seen[f.Target] = true
		rows, e := tx.QueryContext(ctx, `SELECT target,job_id,plan_version FROM target_reservations WHERE target=? OR substr(target,1,length(?)+1)=?||'/' OR substr(?,1,length(target)+1)=target||'/'`, f.Target, f.Target, f.Target, f.Target)
		if e != nil {
			return e
		}
		for rows.Next() {
			var target, id string
			var v int
			if e = rows.Scan(&target, &id, &v); e != nil {
				rows.Close()
				return e
			}
			if target != f.Target || id != j.ID || v != j.Plan.Version {
				rows.Close()
				return errors.New("target_reserved")
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO target_reservations VALUES(?,?,?)", f.Target, j.ID, j.Plan.Version); e != nil {
			return e
		}
		state := "pending"
		if j.Import == "imported" {
			state = "linked"
		}
		if _, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO file_execution(job_id,plan_version,file_index,state,video,discovered) VALUES(?,?,?,?,?,?)", j.ID, j.Plan.Version, i, state, videoTarget(f.Target), j.Discovery == "confirmed"); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) FileExecutions(ctx context.Context, id string, version int) ([]FileExecution, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT file_index,state,video,discovered FROM file_execution WHERE job_id=? AND plan_version=? ORDER BY file_index", id, version)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []FileExecution
	for rows.Next() {
		var f FileExecution
		if e = rows.Scan(&f.Index, &f.State, &f.Video, &f.Discovered); e != nil {
			return nil, e
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// MarkFile 将逐文件事实、job CAS 和可重放事件绑定在一个事务中。
func (s *Store) MarkFile(ctx context.Context, j *Job, index int, kind string) error {
	return s.save(ctx, j, kind, &index)
}
func advanceFileTx(ctx context.Context, tx *sql.Tx, j *Job, index int, kind string) error {
	if j.Plan == nil || index < 0 || index >= len(j.Plan.Files) {
		return errors.New("invalid_file_index")
	}
	var r sql.Result
	var e error
	switch kind {
	case "file_intent":
		r, e = tx.ExecContext(ctx, "UPDATE file_execution SET state='intended' WHERE job_id=? AND plan_version=? AND file_index=? AND state='pending'", j.ID, j.Plan.Version, index)
	case "file_linked":
		r, e = tx.ExecContext(ctx, "UPDATE file_execution SET state='linked' WHERE job_id=? AND plan_version=? AND file_index=? AND state='intended'", j.ID, j.Plan.Version, index)
	case "video_discovered":
		r, e = tx.ExecContext(ctx, "UPDATE file_execution SET discovered=1 WHERE job_id=? AND plan_version=? AND file_index=? AND state='linked' AND video=1 AND discovered=0", j.ID, j.Plan.Version, index)
	default:
		return errors.New("invalid_file_event")
	}
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return errors.New("stale_file_state")
	}
	return nil
}
