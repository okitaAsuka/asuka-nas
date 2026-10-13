package nas

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 真实管理API+SQLite，复用成功人工记录；控制端仅在测试回环服务暴露。
func TestRuleProposalBrowserLoop(t *testing.T) {
	if os.Getenv("NAS_BROWSER_TEST") != "1" {
		t.Skip("run make verify-ui")
	}
	ctx := context.Background()
	type scenario struct {
		f        *fixture
		proposal RuleProposal
		input    ProposalInput
		url      string
	}
	scenarios := map[string]*scenario{}
	for _, name := range []string{"approve", "reject", "stale", "epoch", "conflict", "pages"} {
		f, _, in := learningFixture(t)
		// 模型fake的有限数据经生产学习服务提交；浏览器从未拿到学习Bearer。
		model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(in) }))
		res, err := model.Client().Get(model.URL)
		if err != nil {
			t.Fatal(err)
		}
		var modeled ProposalInput
		if err = json.NewDecoder(res.Body).Decode(&modeled); err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		model.Close()
		modeled.Uncertainties = append(modeled.Uncertainties, "<img src=x onerror=window.PWNED=1>")
		body, _ := json.Marshal(modeled)
		req := httptest.NewRequest("POST", "http://127.0.0.1:8789/v1/learning/proposals", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer fixture-learning-token-01234567890123456789")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		LearningHandler(f.store, "fixture-learning-token-01234567890123456789", "127.0.0.1:8789").ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		items, _, err := f.store.ProposalList(ctx, 0)
		if err != nil || len(items) != 1 {
			t.Fatal(err, items)
		}
		p := items[0]
		if name == "pages" {
			for i := 0; i < 22; i++ {
				modeled.Explanation = fmt.Sprintf("人工审核版本%d", i)
				p, err = f.store.ReviseProposal(ctx, p.ID, p.Revision, p.Version, modeled)
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		if name == "conflict" {
			rule := in.Rule
			rule.ID = "conflicting"
			rule.Revision = 1
			rule.Directory = "Other"
			if err = f.store.AddRule(ctx, rule); err != nil {
				t.Fatal(err)
			}
			if err = f.store.SetRule(ctx, rule.ID, 1, true); err != nil {
				t.Fatal(err)
			}
		}
		_, web := managementFixture(t, f)
		scenarios[name] = &scenario{f, p, in, web.URL}
	}
	var nextID string
	scopeType, hasScope := reflect.TypeOf(RuleProposal{}).FieldByName("ScopeDeclaration")
	_ = scopeType
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		switch r.URL.Path {
		case "/scenarios":
			urls := map[string]string{}
			for name, s := range scenarios {
				urls[name] = s.url
			}
			json.NewEncoder(w).Encode(map[string]any{"urls": urls, "scope": hasScope})
			return
		case "/epoch":
			s := scenarios["epoch"]
			rule := s.input.Rule
			rule.ID = "other"
			rule.Revision = 1
			rule.SeriesID = "other"
			rule.Aliases = []string{"Other"}
			err = s.f.store.AddRule(ctx, rule)
			if err == nil {
				err = s.f.store.SetRule(ctx, rule.ID, 1, true)
			}
		case "/stale":
			f := scenarios["stale"].f
			j := f.job()
			j.ContextVersion++
			err = f.store.Save(ctx, &j, "source_context_added")
		case "/next":
			f := scenarios["approve"].f
			f.mu.Lock()
			f.hash = "1123456789abcdef0123456789abcdef01234567"
			f.name = "Example [02].mkv"
			err = os.WriteFile(filepath.Join(f.downloads, f.name), []byte("next video"), 0600)
			f.mu.Unlock()
			if err == nil {
				nextID, err = f.store.IngestRelease(ctx, "fixture", "next-guid", f.hash, "magnet:?xt=urn:btih:"+f.hash, nil, 7, "Example [02]")
			}
			if err == nil {
				err = f.engine.Tick(ctx, time.Unix(1003, 0))
			}
			if err == nil {
				err = f.engine.Tick(ctx, time.Unix(1004, 0))
			}
		default:
			w.WriteHeader(404)
			return
		}
		if err != nil {
			t.Errorf("fixture control %s: %v", r.URL.Path, err)
			w.WriteHeader(500)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	defer control.Close()
	command := exec.Command("python3", "../../tests/proposal_browser.py")
	command.Env = append(os.Environ(), "NAS_FIXTURE_URL="+control.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
	approved, err := scenarios["approve"].f.store.Proposal(ctx, scenarios["approve"].proposal.ID, 2)
	if err != nil || approved.State != "approved" {
		t.Fatal("approval not durable", approved, err)
	}
	for _, name := range []string{"reject", "stale", "conflict"} {
		s := scenarios[name]
		epoch, e := s.f.store.NamingEpoch(ctx)
		if e != nil || (name != "conflict" && epoch != 0) {
			t.Fatal("unexpected rule write", name, epoch, e)
		}
	}
	if hasScope {
		f := scenarios["approve"].f
		next, e := f.store.Get(ctx, nextID)
		if e != nil || next.Import != "imported" || next.Discovery != "confirmed" || next.Plan == nil || next.Plan.Files[0].Target != "Example/Season 02/Example - S02E02.mkv" {
			t.Fatal("next episode did not use approved rule", next, e)
		}
		src, e := os.Stat(filepath.Join(f.downloads, f.name))
		if e != nil {
			t.Fatal(e)
		}
		dst, e := os.Stat(filepath.Join(f.library, next.Plan.Files[0].Target))
		if e != nil || !os.SameFile(src, dst) {
			t.Fatal("next episode not hardlink", e)
		}
	}
}
