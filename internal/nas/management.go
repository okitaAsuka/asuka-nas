package nas

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var webFiles embed.FS

// Management 仅是本机单用户入口，不把局域网视作认证。
type Management struct {
	Daemon             *Daemon
	origin, host, csrf string
	assets             http.Handler
	writes             chan struct{}
}

func NewManagement(ctx context.Context, d *Daemon, host string) (*Management, error) {
	h, _, err := net.SplitHostPort(host)
	ip := net.ParseIP(h)
	if err != nil || ip == nil || !ip.IsLoopback() {
		return nil, errors.New("loopback_required")
	}
	if err = d.Engine.Store.InitControl(ctx, d.Config.Subscriptions); err != nil {
		return nil, err
	}
	d.Managed = true
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, errors.New("csrf_unavailable")
	}
	files, _ := fs.Sub(webFiles, "web")
	return &Management{Daemon: d, origin: "http://" + host, host: host, csrf: hex.EncodeToString(nonce[:]), assets: http.FileServer(http.FS(files)), writes: make(chan struct{}, 1)}, nil
}

// ListenManagement 不接受 localhost DNS/通配地址；调用者必须显式选择 serve。
func ListenManagement(address string) (net.Listener, error) {
	h, _, err := net.SplitHostPort(address)
	ip := net.ParseIP(h)
	if err != nil || ip == nil || !ip.IsLoopback() {
		return nil, errors.New("loopback_required")
	}
	return net.Listen("tcp", address)
}
func (m *Management) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if r.Host != m.host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != m.origin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		apiError(w, 403, "origin_rejected")
		return
	}
	if r.Method != "GET" && r.Method != "POST" {
		apiError(w, 405, "method_rejected")
		return
	}
	// 人工管理身份与服务Bearer完全分离，防止借batch/legacy入口自批模型建议。
	if r.Method == "POST" && (r.Header.Get("Authorization") != "" || r.Header.Get("Origin") != m.origin || r.Header.Get("Content-Type") != "application/json" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Nonce")), []byte(m.csrf)) != 1) {
		apiError(w, 403, "csrf_rejected")
		return
	}
	if r.Method == "POST" {
		select {
		case m.writes <- struct{}{}:
			defer func() { <-m.writes }()
		default:
			apiError(w, 429, "operation_busy")
			return
		}
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		m.api(w, r)
		return
	}
	if r.Method != "GET" || (r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/style.css") {
		apiError(w, 404, "not_found")
		return
	}
	m.assets.ServeHTTP(w, r)
}
func apiJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func apiError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// 只发布固定领域码；不把驱动、HTTP 或用户输入的原始错误送到浏览器。
func publicError(err error) string {
	if err == nil {
		return ""
	}
	code := err.Error()
	for _, v := range strings.Fields(`naming_stale naming_invalid naming_not_found naming_retry_required naming_limit naming_rules_available naming_unresolved naming_preview_required naming_replay_conflict stale_version stale_plan plan_frozen invalid_job_state fingerprint_changed conflict cross_device permission_denied target_reserved unsafe_path unknown_source invalid_subscription subscription_limit subscription_history_limit subscription_exists subscription_not_found subscription_frozen profile_unavailable rule_invalid rule_invalid_range rule_not_normalized rule_revision_exists rule_not_found series_order_conflict rule_disabled rule_scope_ambiguous title_file_conflict season_conflict absolute_mapping_unresolved source_mapping_unresolved mapping_conflict rule_normalization_unresolved collection_incomplete collection_preview_only episode_unresolved special_episode unsupported_manifest awaiting_plan_confirmation legacy_evidence_unresolved source_disabled invalid_request library_unavailable library_unsafe_path library_read_failed library_limit library_cancelled library_changed candidate_stale invalid_batch order_confirmation_required episode_out_of_bounds duplicate_episode invalid_file_manifest collection_preview_only unsupported_episode_kind unsupported_video unsupported_file_set subtitle_mapping_ambiguous episode_unresolved target_conflict comparison_limit sample_stale proposal_invalid proposal_unbounded proposal_evidence_conflict proposal_not_found proposal_revision_exists proposal_replay_conflict proposal_limit proposal_stale proposal_trial_stale proposal_conflict proposal_trial_limit`) {
		if code == v {
			return v
		}
	}
	return "operation_failed"
}
func decodeRequest(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid_request")
	}
	return nil
}

// 公开 DTO 显式列字段，避免内部 Job 将来新增凭据时自动泄露。
type publicJob struct {
	ID          string   `json:"id"`
	Version     int      `json:"version"`
	Download    string   `json:"download"`
	Import      string   `json:"import"`
	Refresh     string   `json:"refresh"`
	Discovery   string   `json:"discovery"`
	Seed        string   `json:"seed"`
	CompletedAt int64    `json:"completed_at"`
	StopAt      int64    `json:"stop_at"`
	SeedDays    int      `json:"seed_days"`
	NextAttempt int64    `json:"next_attempt"`
	ErrorCode   string   `json:"error_code"`
	BlockReason string   `json:"block_reason"`
	Sources     []string `json:"sources,omitempty"`
	Plan        *Plan    `json:"plan,omitempty"`
}
type sourceContext struct {
	Subscription string `json:"subscription"`
	Title        string `json:"title"`
}
type batchHint struct {
	Source   string `json:"source"`
	Season   int    `json:"season"`
	Relative int    `json:"relative"`
	Absolute int    `json:"absolute"`
	Problem  string `json:"problem,omitempty"`
}
type jobDetail struct {
	BatchHints []batchHint     `json:"batch_hints,omitempty"`
	Job        publicJob       `json:"job"`
	Digest     string          `json:"digest"`
	Files      []FileExecution `json:"files"`
	Ready      bool            `json:"ready"`
	Contexts   []sourceContext `json:"contexts,omitempty"`
}

func (m *Management) detail(ctx context.Context, id string) (jobDetail, error) {
	s := m.Daemon.Engine.Store
	j, err := s.Get(ctx, id)
	if err != nil {
		return jobDetail{}, err
	}
	v := jobDetail{Job: publicJob{ID: j.ID, Version: j.Version, Download: j.Download, Import: j.Import, Refresh: j.Refresh, Discovery: j.Discovery, Seed: j.Seed, CompletedAt: j.CompletedAt, StopAt: j.StopAt, SeedDays: j.SeedDays, NextAttempt: j.NextAttempt, ErrorCode: j.ErrorCode, BlockReason: j.BlockReason, Sources: j.Sources, Plan: j.Plan}, Files: []FileExecution{}, Ready: j.Import == "imported" && j.Discovery == "confirmed" && j.BlockReason == ""}
	if j.Plan != nil {
		v.Digest = PlanDigest(j.Plan)
		v.Files, err = s.FileExecutions(ctx, id, j.Plan.Version)
		if err != nil {
			return v, err
		}
	}
	for _, source := range j.Sources {
		if !videoTarget(source) {
			continue
		}
		p, err := ParseTitle(source)
		hint := batchHint{Source: source, Season: p.Season}
		if err != nil {
			hint.Problem = publicError(err)
		} else {
			if p.Collection {
				hint.Problem = "collection_preview_only"
			}
			if p.Relative != nil {
				hint.Relative = p.Relative.Start
			}
			if p.Absolute != nil {
				hint.Absolute = p.Absolute.Start
			}
		}
		v.BatchHints = append(v.BatchHints, hint)
	}
	// GUID 可以来自秘密 enclosure URL，绝不直接输出 release_context。
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT subscription,title FROM release_context WHERE job_id=? ORDER BY subscription,title LIMIT 100", id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var c sourceContext
		if err = rows.Scan(&c.Subscription, &c.Title); err != nil {
			return v, err
		}
		v.Contexts = append(v.Contexts, c)
	}
	return v, rows.Err()
}
func (m *Management) api(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s := m.Daemon.Engine.Store
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	if m.namingAPI(w, r, path) {
		return
	}
	if m.proposalAPI(w, r, path) {
		return
	}
	if r.Method == "GET" {
		switch path {
		case "session":
			apiJSON(w, map[string]string{"csrf": m.csrf})
			return
		case "health":
			err := s.db.PingContext(ctx)
			if err != nil {
				apiError(w, 503, "database_unavailable")
				return
			}
			apiJSON(w, map[string]any{"mode": "local_single_user", "database": "ok", "gateway": "not_connected", "discord": "not_connected", "metadata": "not_connected", "learning": "local_evidence_candidates", "model": "not_connected"})
			return
		case "subscriptions":
			v, next, total, err := s.SubscriptionPage(ctx, r.URL.Query().Get("after"))
			if err != nil {
				apiError(w, 500, "store_failed")
				return
			}
			profiles := []string{}
			for _, p := range m.Daemon.Config.Subscriptions {
				profiles = append(profiles, p.ID)
				for i := range v {
					if v[i].Profile == p.ID {
						v[i].Available = true
					}
				}
			}
			apiJSON(w, map[string]any{"items": v, "profiles": profiles, "next": next, "total": total})
			return
		case "jobs":
			after := r.URL.Query().Get("after")
			rows, err := s.db.QueryContext(ctx, "SELECT id FROM jobs WHERE id>? ORDER BY id LIMIT 101", after)
			if err != nil {
				apiError(w, 500, "store_failed")
				return
			}
			ids := []string{}
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					break
				}
				ids = append(ids, id)
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if err != nil {
				apiError(w, 500, "store_failed")
				return
			}
			next := ""
			if len(ids) > 100 {
				ids = ids[:100]
				next = ids[99]
			}
			out := []jobDetail{}
			for _, id := range ids {
				v, e := m.detail(ctx, id)
				if e != nil {
					apiError(w, 500, "store_failed")
					return
				}
				v.Job.Sources = nil
				v.Contexts = nil
				v.BatchHints = nil
				v.Job.Plan = nil
				v.Files = nil
				v.Digest = ""
				out = append(out, v)
			}
			apiJSON(w, map[string]any{"items": out, "next": next})
			return
		case "comparisons":
			after, err := strconv.Atoi(r.URL.Query().Get("after"))
			if r.URL.Query().Get("after") == "" {
				after = 0
				err = nil
			}
			if err != nil || after < 0 {
				apiError(w, 400, "invalid_request")
				return
			}
			items, next, err := s.ComparisonPage(ctx, r.URL.Query().Get("id"), after)
			if err != nil {
				apiError(w, 409, publicError(err))
				return
			}
			apiJSON(w, map[string]any{"items": items, "next": next})
			return
		case "epoch":
			v, err := s.NamingEpoch(ctx)
			if err != nil {
				apiError(w, 500, "store_failed")
				return
			}
			apiJSON(w, map[string]int64{"epoch": v})
			return
		case "rules":
			v, err := s.RuleViews(ctx)
			if err != nil {
				apiError(w, 500, "store_failed")
				return
			}
			apiJSON(w, v)
			return
		case "audit":
			v, err := s.Audit(ctx)
			if err != nil {
				apiError(w, 500, "store_failed")
				return
			}
			apiJSON(w, v)
			return
		case "events", "source-events":
			after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
			if r.URL.Query().Get("after") == "" {
				err = nil
			}
			if err != nil || after < 0 {
				apiError(w, 400, "invalid_cursor")
				return
			}
			var v any
			if path == "events" {
				v, err = s.Events(ctx, after)
			} else {
				v, err = s.SourceEvents(ctx, after)
			}
			if err != nil {
				apiError(w, 500, "store_failed")
				return
			}
			apiJSON(w, v)
			return
		}
		if strings.HasPrefix(path, "jobs/") {
			v, err := m.detail(ctx, strings.TrimPrefix(path, "jobs/"))
			if err != nil {
				apiError(w, 404, "job_not_found")
				return
			}
			apiJSON(w, v)
			return
		}
		apiError(w, 404, "not_found")
		return
	}
	var err error
	switch path {
	case "subscriptions":
		var v ManagedSubscription
		if err = decodeRequest(w, r, &v); err == nil {
			m.Daemon.sourceMu.Lock()
			err = s.PutSubscription(ctx, v)
			m.Daemon.sourceMu.Unlock()
		}
	case "source-retry":
		var v struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		}
		if err = decodeRequest(w, r, &v); err == nil {
			m.Daemon.sourceMu.Lock()
			var enabled bool
			var version int
			err = s.db.QueryRowContext(ctx, "SELECT enabled,version FROM managed_subscriptions WHERE id=?", v.ID).Scan(&enabled, &version)
			if err == nil {
				if !enabled {
					err = errors.New("source_disabled")
				} else if version != v.Version {
					err = errors.New("stale_version")
				} else {
					err = s.RetrySource(ctx, v.ID, v.Version)
				}
			}
			m.Daemon.sourceMu.Unlock()
		}
	case "library-candidates", "candidate-rule-preview", "candidate-rule":
		var v struct {
			Directory string     `json:"directory"`
			Digest    string     `json:"digest"`
			Rule      NamingRule `json:"rule"`
			Title     string     `json:"title"`
			Source    string     `json:"source"`
		}
		if err = decodeRequest(w, r, &v); err == nil {
			var candidates LibraryCandidates
			candidates, err = discoverLibrary(ctx, m.Daemon.Config.Library, v.Directory)
			if err == nil && path == "library-candidates" {
				apiJSON(w, candidates)
				return
			}
			if err == nil && (v.Digest == "" || v.Digest != candidates.Digest || v.Rule.Directory != v.Directory) {
				err = errors.New("candidate_stale")
			}
			if err == nil {
				err = v.Rule.Validate()
			}
			if err == nil && path == "candidate-rule-preview" {
				var decision NamingDecision
				decision, err = v.Rule.Resolve(v.Title, v.Source)
				if err == nil {
					apiJSON(w, map[string]any{"decision": decision, "explanation": "已有库仅提供局部证据；此草稿须人工确认篇章季号与集序，保存后仍需单独启用。"})
					return
				}
			}
			if err == nil && path == "candidate-rule" {
				m.Daemon.Engine.mu.Lock()
				err = s.AddRule(ctx, v.Rule)
				m.Daemon.Engine.mu.Unlock()
			}
		}
	case "rules":
		var v NamingRule
		if err = decodeRequest(w, r, &v); err == nil {
			m.Daemon.Engine.mu.Lock()
			err = s.AddRule(ctx, v)
			m.Daemon.Engine.mu.Unlock()
		}
	case "rule-state":
		var v struct {
			ID       string `json:"id"`
			Revision int    `json:"revision"`
			Enabled  bool   `json:"enabled"`
			Epoch    int64  `json:"epoch"`
		}
		if err = decodeRequest(w, r, &v); err == nil {
			m.Daemon.Engine.mu.Lock()
			var epoch int64
			epoch, err = s.NamingEpoch(ctx)
			if err == nil && epoch != v.Epoch {
				err = errors.New("stale_version")
			}
			if err == nil {
				err = s.SetRule(ctx, v.ID, v.Revision, v.Enabled)
			}
			m.Daemon.Engine.mu.Unlock()
		}
	case "rule-preview":
		var v struct {
			Rule   NamingRule `json:"rule"`
			Title  string     `json:"title"`
			Source string     `json:"source"`
		}
		if err = decodeRequest(w, r, &v); err == nil {
			err = v.Rule.Validate()
			if err == nil {
				var decision NamingDecision
				decision, err = v.Rule.Resolve(v.Title, v.Source)
				if err == nil {
					apiJSON(w, map[string]any{"decision": decision, "explanation": "仅按此草稿的订阅/别名、显式季号和集数边界解析；启用后仍核对所有来源及清单。"})
					return
				}
			}
		}
	case "batch-preview", "batch-confirm":
		var v struct {
			ID           string            `json:"id"`
			Version      int               `json:"version"`
			PlanVersion  int               `json:"plan_version"`
			Digest       string            `json:"digest"`
			Organization BatchOrganization `json:"organization"`
		}
		if err = decodeRequest(w, r, &v); err == nil {
			if path == "batch-preview" {
				var preview *BatchPreview
				preview, err = m.Daemon.Engine.BatchPreview(ctx, v.ID, v.Version, v.Organization)
				if err == nil {
					apiJSON(w, preview)
					return
				}
			} else {
				err = m.Daemon.Engine.BatchConfirm(ctx, v.ID, v.Version, v.PlanVersion, v.Digest, v.Organization)
			}
		}
	case "job-action":
		var v struct {
			ID          string `json:"id"`
			Version     int    `json:"version"`
			Action      string `json:"action"`
			Source      string `json:"source"`
			Target      string `json:"target"`
			PlanVersion int    `json:"plan_version"`
			Digest      string `json:"digest"`
		}
		if err = decodeRequest(w, r, &v); err == nil {
			e := m.Daemon.Engine
			e.mu.Lock()
			var j Job
			j, err = s.Get(ctx, v.ID)
			if err == nil && j.Version != v.Version {
				err = errors.New("stale_version")
			}
			if err == nil {
				switch v.Action {
				case "retry":
					j.NextAttempt = 0
					err = s.Save(ctx, &j, "manual_retry_requested")
				case "replan":
					err = s.Replan(ctx, v.ID)
				case "preview":
					_, err = e.propose(ctx, v.ID, v.Source, v.Target)
				case "confirm":
					err = e.confirm(ctx, v.ID, v.PlanVersion, v.Digest)
				default:
					err = errors.New("invalid_request")
				}
			}
			e.mu.Unlock()
		}
	default:
		apiError(w, 404, "not_found")
		return
	}
	if err != nil {
		apiError(w, 409, publicError(err))
		return
	}
	apiJSON(w, map[string]bool{"ok": true})
}

// ServeManagement 的关闭顺序由调用者等待：先取消 daemon 和 HTTP，再关闭文件/数据库。
func ServeManagement(ctx context.Context, l net.Listener, h http.Handler) error {
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{Handler: h, BaseContext: func(net.Listener) context.Context { return requestCtx }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	drained := make(chan struct{})
	go func() { <-requestCtx.Done(); _ = server.Shutdown(context.Background()); close(drained) }()
	err := server.Serve(l)
	cancel()
	<-drained
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
