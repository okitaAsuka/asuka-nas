package nas

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const workflowLimit = 10000

// 工作台只读投影有界；不读取完整计划/指纹、不调用上游或改变命名状态。
type workflowItem struct {
	Job         publicJob `json:"job"`
	Ready       bool      `json:"ready"`
	Summary     string    `json:"summary"`
	SummaryKind string    `json:"summary_kind"`
	Episode     string    `json:"episode"`
	NextStep    string    `json:"next_step"`
}
type workflowSnapshot struct {
	Items        []workflowItem
	Total        int
	PendingRules int
}

var summaryURL = regexp.MustCompile(`(?i)(https?://|magnet:)[^\s<>"']+`)
var summarySecret = regexp.MustCompile(`(?i)(token|password|passwd|authorization|api[_-]?key)\s*[:=]\s*[^\s<>&]+`)

func safeWorkflowText(raw string) string {
	raw = summaryURL.ReplaceAllString(raw, "[链接已隐藏]")
	raw = summarySecret.ReplaceAllString(raw, "[凭据已隐藏]")
	raw = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, raw)
	raw = strings.Join(strings.Fields(raw), " ")
	runes := []rune(raw)
	if len(runes) > 180 {
		raw = string(runes[:180]) + "…"
	}
	return raw
}
func workflowMatches(v workflowItem, filter string) bool {
	j := v.Job
	switch filter {
	case "all":
		return true
	case "downloading":
		return j.Download != "completed"
	case "naming":
		return j.Download == "completed" && j.Import != "imported" && (j.Import == "needs_confirmation" || j.BlockReason != "")
	case "emby":
		return j.Import == "imported" && j.Discovery != "confirmed"
	case "failed":
		return j.ErrorCode != ""
	case "ready":
		return v.Ready
	}
	return false
}
func (s *Store) workflowSnapshot(ctx context.Context) (workflowSnapshot, error) {
	v := workflowSnapshot{Items: []workflowItem{}}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs").Scan(&v.Total); err != nil {
		return v, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM rule_proposals WHERE state='pending'").Scan(&v.PendingRules); err != nil {
		return v, err
	}
	// 只取状态和一个来源摘要；绝不公开hash/magnet/GUID/URL/配置路径。
	rows, err := tx.QueryContext(ctx, `SELECT json_object('id',id,'version',version,
 'download',json_extract(body,'$.download'),'import',json_extract(body,'$.import'),
 'refresh',json_extract(body,'$.refresh'),'discovery',json_extract(body,'$.discovery'),
 'seed',json_extract(body,'$.seed'),'stop_at',json_extract(body,'$.stop_at'),
 'error_code',coalesce(json_extract(body,'$.error_code'),''),'block_reason',coalesce(json_extract(body,'$.block_reason'),'')),
 coalesce(json_extract(body,'$.plan.files[0].target'),''),coalesce(json_extract(body,'$.sources[0]'),''),
 coalesce((SELECT title FROM release_context c WHERE c.job_id=jobs.id ORDER BY subscription,title LIMIT 1),'')
 FROM jobs ORDER BY id LIMIT ?`, workflowLimit)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var body, target, source, title string
		var item workflowItem
		if err = rows.Scan(&body, &target, &source, &title); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(body), &item.Job); err != nil {
			break
		}
		raw := title
		item.SummaryKind = "来源标题"
		if source != "" {
			raw = filepath.Base(source)
			item.SummaryKind = "下载文件"
		}
		if target != "" {
			raw = filepath.Base(target)
			item.SummaryKind = "命名方案"
		}
		item.Summary = safeWorkflowText(raw)
		if item.Summary == "" {
			item.Summary = "名称未知"
			item.SummaryKind = "尚无来源摘要"
		}
		item.Episode = "季集未知 · 需明确依据"
		// 只显示明确SxxExx，不把文件编号或RSS篇章猜成真实季号。
		if p, e := ParseTitle(item.Summary); e == nil && p.Season > 0 && p.Relative != nil && !p.Collection {
			item.Episode = "明确季集 " + formatEpisode(p.Season, p.Relative.Start)
		}
		j := item.Job
		item.Ready = j.Import == "imported" && j.Discovery == "confirmed" && j.BlockReason == ""
		item.NextStep = "查看证据与当前方案"
		if item.Ready {
			item.NextStep = "媒体已就绪"
		} else if workflowMatches(item, "failed") {
			item.NextStep = "核对错误后安全恢复"
		} else if workflowMatches(item, "naming") {
			item.NextStep = "补充事实 / 核对命名"
		} else if workflowMatches(item, "emby") {
			item.NextStep = "核对 Emby 发现"
		} else if workflowMatches(item, "downloading") {
			item.NextStep = "等待下载完成"
		}
		v.Items = append(v.Items, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}
func formatEpisode(season, episode int) string {
	return "S" + padNumber(season) + "E" + padNumber(episode)
}
func padNumber(n int) string {
	s := strconv.Itoa(n)
	if n < 10 {
		return "0" + s
	}
	return s
}

func (m *Management) workflowAPI(w http.ResponseWriter, r *http.Request, path string) bool {
	if r.Method != "GET" || (path != "workflow" && path != "jobs") {
		return false
	}
	filter, query, after := r.URL.Query().Get("filter"), strings.TrimSpace(r.URL.Query().Get("q")), r.URL.Query().Get("after")
	if filter == "" {
		filter = "all"
	}
	validFilter := filter == "all" || filter == "downloading" || filter == "naming" || filter == "emby" || filter == "failed" || filter == "ready"
	if !validFilter || len([]rune(query)) > 256 || len(after) > 128 {
		apiError(w, 400, "invalid_request")
		return true
	}
	v, err := m.Daemon.Engine.Store.workflowSnapshot(r.Context())
	if err != nil {
		apiError(w, 500, "store_failed")
		return true
	}
	scope := map[string]any{"total": v.Total, "scanned": len(v.Items), "limit": workflowLimit, "truncated": v.Total > len(v.Items)}
	if path == "workflow" {
		counts := map[string]int{"all": len(v.Items), "rules": v.PendingRules}
		for _, filter := range []string{"downloading", "naming", "emby", "failed", "ready"} {
			for _, item := range v.Items {
				if workflowMatches(item, filter) {
					counts[filter]++
				}
			}
		}
		apiJSON(w, map[string]any{"counts": counts, "scope": scope})
		return true
	}
	out := []workflowItem{}
	matched := 0
	next := ""
	for _, item := range v.Items {
		if !workflowMatches(item, filter) || (query != "" && !strings.Contains(strings.ToLower(item.Summary+" "+item.Job.ID), strings.ToLower(query))) {
			continue
		}
		matched++
		if item.Job.ID > after && len(out) < 101 {
			out = append(out, item)
		}
	}
	if len(out) > 100 {
		out = out[:100]
		next = out[99].Job.ID
	}
	apiJSON(w, map[string]any{"items": out, "next": next, "matched": matched, "scope": scope})
	return true
}
