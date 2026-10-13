package nas

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// namingAPI 沿用本机人工Origin/CSRF；服务Bearer即使附带nonce也不能请求或批准。
func (m *Management) namingAPI(w http.ResponseWriter, r *http.Request, route string) bool {
	if route != "naming" && !strings.HasPrefix(route, "naming-") {
		return false
	}
	if r.Header.Get("Authorization") != "" {
		apiError(w, 403, "csrf_rejected")
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	e := m.Daemon.Engine
	if route == "naming" && r.Method == "GET" {
		items, err := e.NamingReviews(ctx, r.URL.Query().Get("job_id"))
		if err != nil {
			apiError(w, 409, publicError(err))
		} else {
			apiJSON(w, map[string]any{"items": items})
		}
		return true
	}
	if r.Method != "POST" {
		apiError(w, 404, "not_found")
		return true
	}
	var err error
	var out any = map[string]bool{"ok": true}
	if route == "naming-request" {
		var in struct {
			JobID      string      `json:"job_id"`
			JobVersion int         `json:"job_version"`
			Hints      NamingHints `json:"hints"`
			Retry      bool        `json:"retry"`
		}
		err = decodeRequest(w, r, &in)
		if err == nil {
			out, err = e.RequestNaming(ctx, in.JobID, in.JobVersion, in.Hints, in.Retry)
		}
	} else {
		var in struct {
			ID           string             `json:"id"`
			Version      int                `json:"version"`
			Digest       string             `json:"digest"`
			Organization *BatchOrganization `json:"organization,omitempty"`
		}
		err = decodeRequest(w, r, &in)
		if err == nil {
			switch route {
			case "naming-preview":
				out, err = e.PreviewNaming(ctx, in.ID, in.Version, in.Organization)
			case "naming-confirm":
				err = e.ConfirmNaming(ctx, in.ID, in.Version, in.Digest)
			case "naming-reject":
				err = e.RejectNaming(ctx, in.ID, in.Version)
			default:
				apiError(w, 404, "not_found")
				return true
			}
		}
	}
	if err != nil {
		apiError(w, 409, publicError(err))
	} else {
		apiJSON(w, out)
	}
	return true
}

// namingService 只导出人工授权快照和接受结果；共享学习服务鉴权，不包含任何人工管理路由。
func namingService(w http.ResponseWriter, r *http.Request, e *Engine) bool {
	if !strings.HasPrefix(r.URL.Path, "/v1/naming/") {
		return false
	}
	if e == nil {
		apiError(w, 503, "naming_unavailable")
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if r.Method == "GET" && r.URL.Path == "/v1/naming/requests" {
		q := r.URL.Query()
		after, err := strconv.ParseInt(q.Get("after"), 10, 64)
		if err != nil || after < 0 || len(q) != 1 || len(q["after"]) != 1 {
			apiError(w, 400, "invalid_cursor")
			return true
		}
		// 单次页只包含显式请求；端点不能按任意job读取NAS事实。
		rows, err := e.Store.db.QueryContext(ctx, `SELECT seq,snapshot FROM naming_requests WHERE seq>? ORDER BY seq LIMIT 8`, after)
		if err != nil {
			apiError(w, 503, "naming_unavailable")
			return true
		}
		type entry struct {
			Seq      int64          `json:"seq"`
			Snapshot NamingSnapshot `json:"snapshot"`
		}
		items := []entry{}
		next := after
		for rows.Next() {
			var v entry
			var b string
			if err = rows.Scan(&v.Seq, &b); err != nil {
				break
			}
			if err = json.Unmarshal([]byte(b), &v.Snapshot); err != nil {
				break
			}
			items = append(items, v)
			next = v.Seq
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			apiError(w, 503, "naming_unavailable")
		} else {
			apiJSON(w, map[string]any{"protocol": 1, "items": items, "next_cursor": next})
		}
		return true
	}
	if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
		apiError(w, 404, "not_found")
		return true
	}
	switch r.URL.Path {
	case "/v1/naming/check":
		var in struct {
			Protocol int    `json:"protocol"`
			ID       string `json:"id"`
			Digest   string `json:"digest"`
		}
		if decodeRequest(w, r, &in) != nil || in.Protocol != 1 {
			apiError(w, 400, "invalid_request")
			return true
		}
		e.mu.Lock()
		v, b, err := e.Store.namingReview(ctx, in.ID)
		if err == nil && (v.State != "requested" || v.Snapshot.Digest != in.Digest) {
			err = errors.New("naming_stale")
		}
		if err == nil {
			err = e.currentNaming(ctx, v, b)
		}
		e.mu.Unlock()
		if err != nil {
			apiError(w, 409, "naming_stale")
		} else {
			apiJSON(w, map[string]any{"protocol": 1, "ok": true})
		}
	case "/v1/naming/results":
		var in NamingResult
		err := decodeRequest(w, r, &in)
		var v NamingReview
		if err == nil {
			v, err = e.SubmitNaming(ctx, in)
		}
		if err != nil {
			apiError(w, 409, publicError(err))
		} else {
			apiJSON(w, map[string]any{"protocol": 1, "id": v.ID, "state": v.State})
		}
	default:
		apiError(w, 404, "not_found")
	}
	return true
}
