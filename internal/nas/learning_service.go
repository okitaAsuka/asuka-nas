package nas

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"
)

// LearningServiceConfig 仅绑定专用回环端口和既有env引用；默认关闭。
var envReference = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

type LearningServiceConfig struct {
	Enabled  bool   `json:"enabled"`
	Listen   string `json:"listen,omitempty"`
	TokenEnv string `json:"token_env,omitempty"`
}

// Address 不修改事件或浏览器监听。
func (c LearningServiceConfig) Address() string {
	if c.Listen == "" {
		return "127.0.0.1:8789"
	}
	return c.Listen
}

// Validate 不读取或创建凭据；拒绝DNS/公网监听。
func (c LearningServiceConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	h, _, e := net.SplitHostPort(c.Address())
	ip := net.ParseIP(h)
	if e != nil || ip == nil || !ip.IsLoopback() {
		return errors.New("learning_loopback_required")
	}
	if !envReference.MatchString(c.TokenEnv) {
		return errors.New("learning_token_env_required")
	}
	return nil
}

// OpenLearningService 与只读event身份隔离，拒绝复用实际token，不创建持续凭据。
func OpenLearningService(c LearningServiceConfig, events EventServiceConfig, s *Store) (net.Listener, http.Handler, error) {
	if !c.Enabled {
		return nil, nil, errors.New("learning_disabled")
	}
	if e := c.Validate(); e != nil {
		return nil, nil, e
	}
	token := os.Getenv(c.TokenEnv)
	if len(token) < 32 || len(token) > 4096 {
		return nil, nil, errors.New("learning_credentials_unavailable")
	}
	if events.Enabled && (c.TokenEnv == events.TokenEnv || token == os.Getenv(events.TokenEnv)) {
		return nil, nil, errors.New("learning_identity_reused")
	}
	l, e := ListenManagement(c.Address())
	if e != nil {
		return nil, nil, e
	}
	return l, LearningHandler(s, token, l.Addr().String()), nil
}

// LearningHandler agent只能读样本和提交pending；不提供审核/文件/管理接口。
func LearningHandler(s *Store, token, host string) http.Handler {
	expected := sha256.Sum256([]byte("Bearer " + token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if len(token) < 32 || r.Host != host || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
			apiError(w, 401, "service_unauthorized")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if r.URL.Path == "/v1/learning/check" && r.Method == "POST" {
			var in struct {
				Protocol int         `json:"protocol"`
				Samples  []SampleRef `json:"samples"`
			}
			if r.Header.Get("Content-Type") != "application/json" || decodeRequest(w, r, &in) != nil || in.Protocol != 1 || len(in.Samples) < 1 || len(in.Samples) > 8 {
				apiError(w, 400, "invalid_request")
				return
			}
			for _, ref := range in.Samples {
				if _, e := currentSample(ctx, s.db, ref); e != nil {
					apiError(w, 409, "sample_stale")
					return
				}
			}
			apiJSON(w, map[string]any{"protocol": 1, "ok": true})
			return
		}
		if r.URL.Path == "/v1/learning/samples" && r.Method == "GET" {
			q := r.URL.Query()
			values := q["after"]
			if len(q) != 1 || len(values) != 1 {
				apiError(w, 400, "invalid_cursor")
				return
			}
			after, e := strconv.ParseInt(values[0], 10, 64)
			if e != nil || after < 0 {
				apiError(w, 400, "invalid_cursor")
				return
			}
			p, e := s.LearningSamples(ctx, after)
			if e != nil {
				apiError(w, 503, "learning_unavailable")
				return
			}
			apiJSON(w, p)
			return
		}
		if r.URL.Path == "/v1/learning/proposals" && r.Method == "POST" {
			if r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
				apiError(w, 400, "invalid_request")
				return
			}
			var in ProposalInput
			e := decodeRequest(w, r, &in)
			var p RuleProposal
			if e == nil {
				p, e = s.SubmitProposal(ctx, in)
			}
			if e != nil {
				apiError(w, 409, publicError(e))
				return
			}
			apiJSON(w, map[string]any{"protocol": 1, "id": p.ID, "revision": p.Revision, "state": p.State})
			return
		}
		apiError(w, 404, "not_found")
	})
}

// proposalAPI 已经过管理Host/Origin/CSRF/单写门；保留前端独立owner。
func (m *Management) proposalAPI(w http.ResponseWriter, r *http.Request, p string) bool {
	s := m.Daemon.Engine.Store
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if r.Method == "GET" && p == "proposals" {
		after, e := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if e != nil || after < 0 {
			apiError(w, 400, "invalid_cursor")
			return true
		}
		items, next, e := s.ProposalList(ctx, after)
		if e != nil {
			apiError(w, 503, "operation_failed")
		} else {
			apiJSON(w, map[string]any{"items": items, "next": next})
		}
		return true
	}
	if r.Method == "GET" && p == "proposal" {
		rev, e := strconv.Atoi(r.URL.Query().Get("revision"))
		if e != nil || rev < 1 {
			apiError(w, 400, "invalid_request")
			return true
		}
		item, e := s.Proposal(ctx, r.URL.Query().Get("id"), rev)
		if e != nil {
			apiError(w, 404, publicError(e))
		} else {
			apiJSON(w, item)
		}
		return true
	}
	if r.Method == "GET" && p == "proposal-audit" {
		rows, e := s.db.QueryContext(ctx, `SELECT seq,revision,kind,created_at FROM proposal_audit WHERE proposal_id=? ORDER BY seq DESC LIMIT 100`, r.URL.Query().Get("id"))
		if e != nil {
			apiError(w, 503, "operation_failed")
			return true
		}
		defer rows.Close()
		type audit struct {
			Seq       int64  `json:"seq"`
			Revision  int    `json:"revision"`
			Kind      string `json:"kind"`
			CreatedAt int64  `json:"created_at"`
		}
		items := []audit{}
		for rows.Next() {
			var a audit
			if e = rows.Scan(&a.Seq, &a.Revision, &a.Kind, &a.CreatedAt); e != nil {
				break
			}
			items = append(items, a)
		}
		if e != nil || rows.Err() != nil {
			apiError(w, 503, "operation_failed")
		} else {
			apiJSON(w, map[string]any{"items": items})
		}
		return true
	}
	if r.Method == "GET" && p == "proposal-samples" {
		rev, e := strconv.Atoi(r.URL.Query().Get("revision"))
		if e != nil || rev < 1 {
			apiError(w, 400, "invalid_request")
			return true
		}
		proposal, e := s.Proposal(ctx, r.URL.Query().Get("id"), rev)
		if e != nil {
			apiError(w, 404, publicError(e))
			return true
		}
		items := []LearningSample{}
		for _, ref := range proposal.Input.Samples {
			var b string
			var v LearningSample
			if e = s.db.QueryRowContext(ctx, `SELECT body FROM learning_samples WHERE sample_id=?`, ref.SampleID).Scan(&b); e != nil {
				break
			}
			if e = json.Unmarshal([]byte(b), &v); e != nil {
				break
			}
			items = append(items, v)
		}
		if e != nil {
			apiError(w, 503, "operation_failed")
		} else {
			apiJSON(w, map[string]any{"items": items})
		}
		return true
	}
	if r.Method != "POST" {
		return false
	}
	switch p {
	case "proposal-revise", "proposal-preview", "proposal-approve", "proposal-reject":
	default:
		return false
	}
	var in struct {
		ID       string         `json:"id"`
		Revision int            `json:"revision"`
		Version  int            `json:"version"`
		Input    *ProposalInput `json:"input,omitempty"`
		Epoch    int64          `json:"epoch,omitempty"`
		Digest   string         `json:"digest,omitempty"`
	}
	e := decodeRequest(w, r, &in)
	var out any = map[string]bool{"ok": true}
	if e == nil {
		switch p {
		case "proposal-revise":
			if in.Input == nil {
				e = errors.New("invalid_request")
			} else {
				out, e = s.ReviseProposal(ctx, in.ID, in.Revision, in.Version, *in.Input)
			}
		case "proposal-preview":
			out, e = s.PreviewProposal(ctx, in.ID, in.Revision, in.Version)
		default:
			e = s.DecideProposal(ctx, in.ID, in.Revision, in.Version, p == "proposal-approve", in.Epoch, in.Digest)
		}
	}
	if e != nil {
		apiError(w, 409, publicError(e))
	} else {
		apiJSON(w, out)
	}
	return true
}
