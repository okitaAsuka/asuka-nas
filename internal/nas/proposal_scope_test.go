package nas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestProposalHumanScopeExplicitAndBounded 原样本仅E01；只有人工显式声明才能扩到E01–12。
func TestProposalHumanScopeExplicitAndBounded(t *testing.T) {
	f, _, in := learningFixture(t)
	ctx := context.Background()
	p, e := f.store.SubmitProposal(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	edit := p.Input
	edit.Rule.Episodes = &EpisodeRange{1, 12}
	if _, e = f.store.SubmitProposal(ctx, edit); e == nil {
		t.Fatal("agent expanded range")
	}
	if _, e = f.store.ReviseProposal(ctx, p.ID, 1, 1, edit); e == nil || e.Error() != "proposal_unbounded" {
		t.Fatal("implicit human expansion", e)
	}
	wrong := &ScopeDeclaration{Episodes: EpisodeRange{1, 11}, Reason: "人工确认"}
	if _, e = f.store.ReviseProposal(ctx, p.ID, 1, 1, edit, wrong); e == nil {
		t.Fatal("scope mismatch")
	}
	scope := &ScopeDeclaration{Episodes: EpisodeRange{1, 12}, Reason: "人工确认本季第一至十二集"}
	p, e = f.store.ReviseProposal(ctx, p.ID, 1, 1, edit, scope)
	if e != nil {
		t.Fatal(e)
	}
	if p.ScopeDeclaration == nil || p.ScopeDeclaration.Episodes.End != 12 {
		t.Fatal("declaration missing")
	}
	trial, e := f.store.PreviewProposal(ctx, p.ID, p.Revision, p.Version)
	if e != nil {
		t.Fatal(e)
	}
	if trial.ScopeReview == nil || len(trial.ScopeReview.VerifiedEpisodes) != 1 || trial.ScopeReview.VerifiedEpisodes[0] != 1 || len(trial.ScopeReview.UnverifiedEpisodes) != 11 || trial.ScopeReview.UnverifiedEpisodes[0] != 2 || trial.ScopeReview.UnverifiedEpisodes[10] != 12 {
		t.Fatal("unseen episodes claimed verified", trial.ScopeReview)
	}
	edit = p.Input
	edit.Rule.Episodes = &EpisodeRange{1, 13}
	if _, e = f.store.ReviseProposal(ctx, p.ID, p.Revision, p.Version, edit, p.ScopeDeclaration); e == nil {
		t.Fatal("mismatched previous declaration reused")
	}
	if _, e = f.store.ReviseProposal(ctx, p.ID, p.Revision, p.Version, p.Input); e == nil {
		t.Fatal("declaration silently inherited")
	}
	var count int
	if e = f.store.db.QueryRow(`SELECT count(*) FROM proposal_audit WHERE kind='proposal_scope_declared'`).Scan(&count); e != nil || count != 1 {
		t.Fatal("missing audit", count, e)
	}
	if e = f.store.DecideProposal(ctx, p.ID, p.Revision, p.Version, true, trial.Epoch, trial.Digest); e != nil {
		t.Fatal(e)
	}
	// 文件方案/人工记录仍是E01；启用仅影响未来的合法命名，不改旧文件。
	if f.job().Plan.Batch.Rows[0].Episode != 1 || comparisons(t, f)[0].Intent.Changes[0].Episode != 1 {
		t.Fatal("scope rewrote sample")
	}
}

func TestProposalHumanScopeAbsoluteAndAuthorization(t *testing.T) {
	f, _, in := learningFixture(t)
	ctx := context.Background()
	p, e := f.store.SubmitProposal(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	edit := p.Input
	edit.Rule.Episodes = &EpisodeRange{1, 12}
	edit.Rule.Absolute = &AbsoluteMapping{1, 12, 1}
	scope := &ScopeDeclaration{Episodes: EpisodeRange{1, 12}, Reason: "人工确认有限总集映射", Absolute: &AbsoluteMapping{1, 12, 1}}
	bad := *scope
	bad.Absolute = &AbsoluteMapping{1, 11, 1}
	if _, e = f.store.ReviseProposal(ctx, p.ID, 1, 1, edit, &bad); e == nil {
		t.Fatal("absolute declaration mismatch")
	}
	long := edit
	long.Rule.Episodes = &EpisodeRange{1, 33}
	long.Rule.Absolute = nil
	if _, e = f.store.ReviseProposal(ctx, p.ID, 1, 1, long, &ScopeDeclaration{Episodes: EpisodeRange{1, 33}, Reason: "过宽范围"}); e == nil {
		t.Fatal("33 episodes accepted")
	}
	token := "fixture-learning-token-01234567890123456789"
	h := LearningHandler(f.store, token, "127.0.0.1:8789")
	body, _ := json.Marshal(edit)
	body = append(body[:len(body)-1], []byte(`,"scope_declaration":{"episodes":{"start":1,"end":12},"reason":"agent"}}`)...)
	req := httptest.NewRequest("POST", "http://127.0.0.1:8789/v1/learning/proposals", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 409 {
		t.Fatal("agent declared human scope", w.Code)
	}
	m, server := managementFixture(t, f)
	request, _ := http.NewRequest("POST", server.URL+"/api/proposal-revise", strings.NewReader(`{}`))
	request.Header.Set("Origin", server.URL)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Nonce", m.csrf)
	request.Header.Set("Authorization", "Bearer "+token)
	res, e := server.Client().Do(request)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("service identity accepted human nonce", res.StatusCode)
	}
	revised, e := f.store.ReviseProposal(ctx, p.ID, 1, 1, edit, scope)
	if e != nil {
		t.Fatal(e)
	}
	trial, e := f.store.PreviewProposal(ctx, revised.ID, 2, 1)
	if e != nil || trial.ScopeReview.Absolute == nil || *trial.ScopeReview.Absolute != *scope.Absolute {
		t.Fatal(trial, e)
	}
}

func TestProposalHumanScopeTotalOffsetPreservesObservedMapping(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, "Example [67].mkv", -1)
	if _, e := f.store.IngestRelease(ctx, "fixture", "total-guid", fixtureHash, "magnet:?xt=urn:btih:"+fixtureHash, nil, 7, "Example - 总第67"); e != nil {
		t.Fatal(e)
	}
	f.tick(1000)
	b := BatchOrganization{Directory: "Example", DisplayName: "Example", Season: 2, Mode: "extracted", Offset: -66, Rows: []BatchRow{{Source: f.name, Episode: 1}}}
	preview := batchPreview(t, f, b)
	batchConfirm(t, f, b, preview)
	f.tick(1001)
	f.mu.Lock()
	f.discovered = true
	f.mu.Unlock()
	f.tick(1002)
	page, e := f.store.LearningSamples(ctx, 0)
	if e != nil || len(page.Items) != 1 {
		t.Fatal("sample missing", e)
	}
	in := ProposalInput{Protocol: 1, Samples: []SampleRef{page.Items[0].Sample.Ref}, Rule: NamingRule{Provider: "manual", SeriesID: "total_example", Order: "custom", Subscription: "fixture", Aliases: []string{"Example"}, Season: 2, Directory: "Example", DisplayName: "Example", Episodes: &EpisodeRange{1, 1}, Absolute: &AbsoluteMapping{67, 67, 1}}, Explanation: "总第67集映射本季第一集"}
	p, e := f.store.SubmitProposal(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	edit := p.Input
	edit.Rule.Episodes = &EpisodeRange{1, 12}
	edit.Rule.Absolute = &AbsoluteMapping{67, 78, 1}
	if _, e = f.store.ReviseProposal(ctx, p.ID, 1, 1, edit); e == nil {
		t.Fatal("implicit offset expansion")
	}
	scope := &ScopeDeclaration{Episodes: EpisodeRange{1, 12}, Absolute: &AbsoluteMapping{67, 78, 1}, Reason: "人工确认总第67至78集对应本季1至12集"}
	bad := edit
	bad.Rule.Absolute = &AbsoluteMapping{66, 77, 1}
	if _, e = f.store.ReviseProposal(ctx, p.ID, 1, 1, bad, &ScopeDeclaration{Episodes: EpisodeRange{1, 12}, Absolute: bad.Rule.Absolute, Reason: scope.Reason}); e == nil {
		t.Fatal("human scope changed observed offset")
	}
	p, e = f.store.ReviseProposal(ctx, p.ID, 1, 1, edit, scope)
	if e != nil {
		t.Fatal(e)
	}
	trial, e := f.store.PreviewProposal(ctx, p.ID, 2, 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.store.DecideProposal(ctx, p.ID, 2, 1, true, trial.Epoch, trial.Digest); e != nil {
		t.Fatal(e)
	}
	r, e := f.store.Rule(ctx, p.ID, 2)
	if e != nil {
		t.Fatal(e)
	}
	d, e := r.Resolve("Example - 总第68", "Example [68].mkv")
	if e != nil || d.Episode != 2 {
		t.Fatal("human bounded offset unused", d, e)
	}
	if _, e = r.Resolve("Example - 总第79", "Example [79].mkv"); e == nil {
		t.Fatal("offset escaped finite declaration")
	}
}

func TestProposalScopeEpochAndNewConflictRejectApproval(t *testing.T) {
	ctx := context.Background()
	f, _, in := learningFixture(t)
	p, e := f.store.SubmitProposal(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	edit := p.Input
	edit.Rule.Episodes = &EpisodeRange{1, 12}
	p, e = f.store.ReviseProposal(ctx, p.ID, 1, 1, edit, &ScopeDeclaration{Episodes: EpisodeRange{1, 12}, Reason: "人工确认有限范围"})
	if e != nil {
		t.Fatal(e)
	}
	old, e := f.store.PreviewProposal(ctx, p.ID, 2, 1)
	if e != nil {
		t.Fatal(e)
	}
	conflict := p.Input.Rule
	conflict.ID = "conflicting_rule"
	conflict.Revision = 1
	conflict.SeriesID = "another_series"
	conflict.Directory = "Another directory"
	installRule(t, f.store, conflict)
	if e = f.store.DecideProposal(ctx, p.ID, 2, 1, true, old.Epoch, old.Digest); e == nil || e.Error() != "proposal_trial_stale" {
		t.Fatal("old epoch approved", e)
	}
	now, e := f.store.PreviewProposal(ctx, p.ID, 2, 1)
	if e != nil || now.Conflicts != 1 {
		t.Fatal("new identity conflict not shown", now.Conflicts, e)
	}
	if e = f.store.DecideProposal(ctx, p.ID, 2, 1, true, now.Epoch, now.Digest); e == nil || e.Error() != "proposal_conflict" {
		t.Fatal("new conflict ignored", e)
	}
	var active int
	if e = f.store.db.QueryRow(`SELECT count(*) FROM naming_active WHERE id=?`, p.ID).Scan(&active); e != nil || active != 0 {
		t.Fatal("failed approval enabled rule")
	}
}

func TestLearningRejectsUnknownRecordProvenance(t *testing.T) {
	f, _, _ := learningFixture(t)
	r := comparisons(t, f)[0].Intent
	for _, change := range []string{"origin", "schema"} {
		v := r
		if change == "origin" {
			v.Origin = "agent"
		} else {
			v.SchemaVersion = 2
		}
		if _, e := sampleFromRecord(context.Background(), f.store.db, v); e == nil || e.Error() != "sample_ineligible" {
			t.Fatal("unknown provenance entered learning", change, e)
		}
	}
}
