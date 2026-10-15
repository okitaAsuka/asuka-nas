package nas

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
)

// EventServiceConfig 是独立只读服务认证，不复用浏览器 nonce；默认关闭且仅回环。
type EventServiceConfig struct {
	Enabled  bool   `json:"enabled"`
	Listen   string `json:"listen,omitempty"`
	TokenEnv string `json:"token_env,omitempty"`
}

// Address 返回专用事件端口，绝不改变管理服务监听。
func (c EventServiceConfig) Address() string {
	if c.Listen == "" {
		return "127.0.0.1:8788"
	}
	return c.Listen
}

// Validate 仅检查配置，不生成或读取秘密。
func (c EventServiceConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	h, _, e := net.SplitHostPort(c.Address())
	ip := net.ParseIP(h)
	if e != nil || ip == nil || !ip.IsLoopback() {
		return errors.New("events_loopback_required")
	}
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`).MatchString(c.TokenEnv) {
		return errors.New("events_token_env_required")
	}
	return nil
}

// OpenEventService 只暴露有界事件页，先验证部署者已有凭据，再创建新回环监听。
func OpenEventService(c EventServiceConfig, store *Store) (net.Listener, http.Handler, error) {
	if !c.Enabled {
		return nil, nil, errors.New("events_disabled")
	}
	if e := c.Validate(); e != nil {
		return nil, nil, e
	}
	token := os.Getenv(c.TokenEnv)
	if len(token) < 32 || len(token) > 4096 {
		return nil, nil, errors.New("events_credentials_unavailable")
	}
	l, e := ListenManagement(c.Address())
	if e != nil {
		return nil, nil, e
	}
	return l, EventHandler(store, token, l.Addr().String()), nil
}

// EventHandler 的明确服务鉴权与精确 Host 拒绝浏览器跨站来源；返回字段没有标题/URL/错误原文。
func EventHandler(store *Store, token, host string) http.Handler {
	return serviceBoundary(token, host, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/events" {
			apiError(w, 404, "not_found")
			return
		}
		if r.Method != "GET" {
			apiError(w, 405, "read_only")
			return
		}
		q, e := r.URL.Query()["after"]
		if !e || len(q) != 1 || len(r.URL.Query()) != 1 {
			apiError(w, 400, "invalid_cursor")
			return
		}
		after, err := strconv.ParseInt(q[0], 10, 64)
		if err != nil || after < 0 {
			apiError(w, 400, "invalid_cursor")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5e9)
		defer cancel()
		events, err := store.Events(ctx, after)
		if err != nil {
			apiError(w, 503, "events_unavailable")
			return
		}
		page := eventPage{Events: make([]wireEvent, 0, len(events)), Next: after}
		for _, v := range events {
			page.Events = append(page.Events, wireEvent{v.ID, v.SchemaVersion, v.JobID, v.Kind, v.PlanVersion, v.FileIndex})
			page.Next = v.ID
		}
		apiJSON(w, page)
	}))
}

// wireEvent 只使用现有安全事件字段，协议版本与领域 schema 分开。
type wireEvent struct {
	ID     int64  `json:"id"`
	Schema int    `json:"schema_version"`
	Job    string `json:"job_id"`
	Kind   string `json:"kind"`
	Plan   *int   `json:"plan_version,omitempty"`
	File   *int   `json:"file_index,omitempty"`
}
type eventPage struct {
	Events []wireEvent `json:"events"`
	Next   int64       `json:"next_cursor"`
}
