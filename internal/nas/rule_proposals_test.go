package nas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func learningFixture(t *testing.T) (*fixture, LearningSample, ProposalInput) {
	t.Helper()
	ctx := context.Background()
	f := newFixture(t, "Example [01].mkv", -1)
	if _, e := f.store.IngestRelease(ctx, "fixture", "private-guid", "0123456789abcdef0123456789abcdef01234567", "magnet:?xt=urn:btih:"+fixtureHash, nil, 7, "Example [01]"); e != nil {
		t.Fatal(e)
	}
	f.tick(1000)
	b := BatchOrganization{Directory: "Example", DisplayName: "Example", Season: 2, Mode: "manual", Rows: []BatchRow{{Source: f.name, Episode: 1}}}
	preview := batchPreview(t, f, b)
	batchConfirm(t, f, b, preview)
	p, e := f.store.LearningSamples(ctx, 0)
	if e != nil || len(p.Items) != 0 {
		t.Fatal("unexecuted human intent exported", p, e)
	}
	f.tick(1001)
	p, e = f.store.LearningSamples(ctx, 0)
	if e != nil || len(p.Items) != 0 {
		t.Fatal("awaiting discovery exported", p, e)
	}
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	f.tick(1002)
	p, e = f.store.LearningSamples(ctx, 0)
	if e != nil || len(p.Items) != 1 {
		t.Fatal("success missing", p, e)
	}
	sample := p.Items[0].Sample
	r := NamingRule{SeriesID: "example", Provider: "manual", Order: "custom", Subscription: "fixture", Aliases: []string{"Example"}, Season: 2, Directory: "Example", DisplayName: "Example", Episodes: &EpisodeRange{1, 1}}
	in := ProposalInput{Protocol: 1, Samples: []SampleRef{sample.Ref}, Rule: r, Explanation: "本次人工明确第二季第一集；只适用已观察范围。", Uncertainties: []string{"不推断未来篇章"}, Exceptions: []string{}}
	return f, sample, in
}
func TestRuleProposalHumanOnlyAtomicApproval(t *testing.T) {
	ctx := context.Background()
	f, sample, in := learningFixture(t)
	original := comparisons(t, f)[0].Intent
	p, e := f.store.SubmitProposal(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	epoch, _ := f.store.NamingEpoch(ctx)
	if epoch != 0 {
		t.Fatal("agent enabled rule")
	}
	for i := 0; i < 3; i++ {
		replay, e := f.store.SubmitProposal(ctx, in)
		if e != nil || replay.ID != p.ID {
			t.Fatal(replay, e)
		}
	}
	trial, e := f.store.PreviewProposal(ctx, p.ID, 1, 1)
	if e != nil || trial.Conflicts != 0 || len(trial.Coverage) != 1 || len(trial.Results) != 1 || trial.Results[0].Outcome != "matched" {
		t.Fatal(trial, e)
	}
	if e = f.store.DecideProposal(ctx, p.ID, 1, 1, true, trial.Epoch, "old"); e == nil {
		t.Fatal("old trial accepted")
	}
	if _, e = f.store.db.Exec(`CREATE TRIGGER fail_proposal BEFORE INSERT ON proposal_audit WHEN NEW.kind='proposal_approved' BEGIN SELECT RAISE(ABORT,'fixture'); END`); e != nil {
		t.Fatal(e)
	}
	if e = f.store.DecideProposal(ctx, p.ID, 1, 1, true, trial.Epoch, trial.Digest); e == nil {
		t.Fatal("transaction failure ignored")
	}
	epoch, _ = f.store.NamingEpoch(ctx)
	if epoch != 0 {
		t.Fatal("approval rollback leaked rule")
	}
	f.store.db.Exec(`DROP TRIGGER fail_proposal`)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- f.store.DecideProposal(ctx, p.ID, 1, 1, true, trial.Epoch, trial.Digest)
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	epoch, _ = f.store.NamingEpoch(ctx)
	if epoch != 1 {
		t.Fatal("duplicate approve advanced epoch", epoch)
	}
	p, e = f.store.Proposal(ctx, p.ID, 1)
	if e != nil || p.State != "approved" || p.Version != 2 {
		t.Fatal(p, e)
	}
	frozen, e := currentSample(ctx, f.store.db, sample.Ref)
	if e != nil || frozen.Ref != sample.Ref {
		t.Fatal("rule changed sample", e)
	}
	if digestStringJSON(original) != digestStringJSON(comparisons(t, f)[0].Intent) {
		t.Fatal("human record changed")
	}
	decision, _, e := f.store.ResolveNaming(ctx, f.job().ID, f.name)
	if e != nil || decision == nil || decision.Season != 2 {
		t.Fatal("enabled rule unused", decision, e)
	}
}
func TestRuleProposalRejectStaleAndRevision(t *testing.T) {
	ctx := context.Background()
	f, sample, in := learningFixture(t)
	wider := in
	wider.Rule.Episodes = &EpisodeRange{1, 2}
	if _, e := f.store.SubmitProposal(ctx, wider); e == nil || e.Error() != "proposal_unbounded" {
		t.Fatal("single sample extrapolated", e)
	}
	p, e := f.store.SubmitProposal(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	edit := p.Input
	edit.Explanation = "人工核对后保留原范围"
	p2, e := f.store.ReviseProposal(ctx, p.ID, 1, 1, edit)
	if e != nil || p2.Revision != 2 {
		t.Fatal(p2, e)
	}
	if _, e = f.store.PreviewProposal(ctx, p.ID, 1, 1); e == nil {
		t.Fatal("superseded preview accepted")
	}
	if e = f.store.DecideProposal(ctx, p.ID, 2, 1, false, 0, ""); e != nil {
		t.Fatal(e)
	}
	epoch, _ := f.store.NamingEpoch(ctx)
	if epoch != 0 {
		t.Fatal("rejection enabled")
	}
	f2, _, in2 := learningFixture(t)
	p, e = f2.store.SubmitProposal(ctx, in2)
	if e != nil {
		t.Fatal(e)
	}
	j := f2.job()
	j.ContextVersion++
	if e = f2.store.Save(ctx, &j, "source_context_added"); e != nil {
		t.Fatal(e)
	}
	p, e = f2.store.Proposal(ctx, p.ID, 1)
	if e != nil || p.State != "stale" {
		t.Fatal(p, e)
	}
	if _, e = currentSample(ctx, f2.store.db, sample.Ref); e == nil {
		t.Fatal("changed source accepted")
	}
}
func TestLearningAuthorizationPrivacyAndSchema(t *testing.T) {
	f, _, in := learningFixture(t)
	token := "fixture-learning-token-01234567890123456789"
	handler := LearningHandler(f.store, token, "127.0.0.1:8789")
	call := func(method, route, bearer, origin, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://127.0.0.1:8789"+route, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, v := range [][2]string{{"wrong", ""}, {token, "http://127.0.0.1:8787"}} {
		if w := call("GET", "/v1/learning/samples?after=0", v[0], v[1], ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	w := call("GET", "/v1/learning/samples?after=0", token, "", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, secret := range []string{"magnet:", "private-guid", f.downloads, f.library, "fake-token", "tracker"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("private context escaped")
		}
	}
	b, _ := json.Marshal(in)
	w = call("POST", "/v1/learning/proposals", token, "", string(b))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, route := range []string{"/api/proposal-approve", "/v1/learning/approve", "/api/session"} {
		if w = call("POST", route, token, "", `{}`); w.Code != http.StatusNotFound {
			t.Fatal("agent has admin capability", route, w.Code)
		}
	}
	invalid := strings.TrimSuffix(string(b), "}") + `,"script":"execute"}`
	if w = call("POST", "/v1/learning/proposals", token, "", invalid); w.Code != 409 {
		t.Fatal("unknown executable field accepted", w.Code)
	}
	for _, c := range []LearningServiceConfig{{Enabled: true, Listen: "0.0.0.0:8789", TokenEnv: "FIXTURE"}, {Enabled: true, Listen: "localhost:8789", TokenEnv: "FIXTURE"}} {
		if c.Validate() == nil {
			t.Fatal("broad listener")
		}
	}
}

// TestProposalManagementDTO 验证前端消费的真实路由与CSRF批准，不用静态mock替代接口。
func TestProposalManagementDTO(t *testing.T) {
	f, _, in := learningFixture(t)
	ctx := context.Background()
	p, e := f.store.SubmitProposal(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	m, server := managementFixture(t, f)
	for _, route := range []string{"proposals?after=0", "proposal?id=" + p.ID + "&revision=1", "proposal-samples?id=" + p.ID + "&revision=1", "proposal-audit?id=" + p.ID} {
		res, e := server.Client().Get(server.URL + "/api/" + route)
		if e != nil {
			t.Fatal(e)
		}
		if res.StatusCode != 200 {
			t.Fatal(route, res.StatusCode)
		}
		res.Body.Close()
	}
	request := map[string]any{"id": p.ID, "revision": 1, "version": 1}
	status, body := postManagement(t, m, server, "proposal-preview", request)
	if status != 200 {
		t.Fatal(status, string(body))
	}
	var trial ProposalTrial
	if json.Unmarshal(body, &trial) != nil || trial.Digest == "" {
		t.Fatal("trial DTO invalid")
	}
	request["epoch"] = trial.Epoch
	request["digest"] = trial.Digest
	status, body = postManagement(t, m, server, "proposal-approve", request)
	if status != 200 {
		t.Fatal(status, string(body))
	}
	request["digest"] = "different"
	status, _ = postManagement(t, m, server, "proposal-approve", request)
	if status != 409 {
		t.Fatal("nonidentical approval replay accepted")
	}
}
