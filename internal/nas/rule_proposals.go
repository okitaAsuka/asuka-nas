package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// ProposalInput 是严格有限数据合同；没有脚本、工具或自动批准字段。
type ProposalInput struct {
	Protocol      int         `json:"protocol"`
	Samples       []SampleRef `json:"samples"`
	Rule          NamingRule  `json:"rule"`
	Explanation   string      `json:"explanation"`
	Uncertainties []string    `json:"uncertainties"`
	Exceptions    []string    `json:"exceptions"`
}

// RuleProposal 的内容不可变，人工修改只能产生下一revision；version用于受控状态CAS。
type RuleProposal struct {
	ID        string        `json:"id"`
	Revision  int           `json:"revision"`
	Version   int           `json:"version"`
	State     string        `json:"state"`
	Origin    string        `json:"origin"`
	CreatedAt int64         `json:"created_at"`
	Input     ProposalInput `json:"input"`
}

// ProposalTrial 明确记录有界覆盖集合；digest绑定全部结果和当前epoch，不承诺未覆盖来源。
type ProposalTrial struct {
	ID        string        `json:"id"`
	Revision  int           `json:"revision"`
	Version   int           `json:"version"`
	Epoch     int64         `json:"epoch"`
	Coverage  []SampleRef   `json:"coverage"`
	Results   []TrialResult `json:"results"`
	Conflicts int           `json:"conflicts"`
	Digest    string        `json:"digest"`
	Limit     int           `json:"limit"`
	Truncated bool          `json:"truncated"`
}

// TrialResult 只在本机授权审核DTO展示文件名，普通事件仅有ID/枚举。
type TrialResult struct {
	SampleID string `json:"sample_id"`
	Source   string `json:"source"`
	Target   string `json:"target,omitempty"`
	Outcome  string `json:"outcome"`
}

func sampleSet(refs []SampleRef) string {
	copyRefs := append([]SampleRef(nil), refs...)
	sort.Slice(copyRefs, func(i, j int) bool { return copyRefs[i].SampleID < copyRefs[j].SampleID })
	return digestStringJSON(copyRefs)
}

// validateProposal 不允许根据单例外推未观察季号/集数；人工也需新增依据才能扩大范围。
func validateProposal(ctx context.Context, q proposalQuery, p ProposalInput) error {
	if p.Protocol != 1 || len(p.Samples) < 1 || len(p.Samples) > 8 || !learningText(p.Explanation, 2000) || len(p.Uncertainties) > 8 || len(p.Exceptions) > 8 {
		return errors.New("proposal_invalid")
	}
	for _, list := range [][]string{p.Uncertainties, p.Exceptions} {
		for _, s := range list {
			if !learningText(s, 512) {
				return errors.New("proposal_invalid")
			}
		}
	}
	r := p.Rule
	if r.IdentityNormalizationVersion != 0 && r.IdentityNormalizationVersion != identityNormalizationVersion {
		return errors.New("proposal_invalid")
	}
	if err := r.Validate(); err != nil {
		return errors.New("proposal_invalid")
	}
	if len(r.Aliases) > 8 || !learningText(r.Directory, 512) || !learningText(r.DisplayName, 200) || r.Group != "" && !learningText(r.Group, 100) {
		return errors.New("proposal_invalid")
	}
	bound := r.bounds()
	if bound == nil || bound.End-bound.Start > 31 {
		return errors.New("proposal_unbounded")
	}
	observed := map[int]bool{}
	aliasEvidence := map[string]bool{}
	seen := map[string]bool{}
	for _, ref := range p.Samples {
		if seen[ref.SampleID] {
			return errors.New("proposal_invalid")
		}
		seen[ref.SampleID] = true
		sample, err := currentSample(ctx, q, ref)
		if err != nil {
			return err
		}
		matchedSample := false
		for _, c := range sample.Contexts {
			if c.Subscription != r.Subscription {
				continue
			}
			title, err := ParseTitle(c.Title)
			if err != nil {
				continue
			}
			for _, a := range r.Aliases {
				if !learningText(a, 512) {
					return errors.New("proposal_invalid")
				}
				if aliasMatched(title, a) {
					aliasEvidence[a] = true
				}
			}
			for _, change := range sample.Changes {
				if !videoTarget(change.Source) {
					continue
				}
				d, err := r.Resolve(c.Title, change.Source)
				if err != nil {
					continue
				}
				if d.Target != change.Target || d.Season != change.Season || d.Episode != change.Episode {
					return errors.New("proposal_evidence_conflict")
				}
				observed[d.Episode] = true
				matchedSample = true
			}
		}
		if !matchedSample {
			return errors.New("proposal_evidence_conflict")
		}
	}
	for _, a := range r.Aliases {
		if !aliasEvidence[a] {
			return errors.New("proposal_evidence_conflict")
		}
	}
	for n := bound.Start; n <= bound.End; n++ {
		if !observed[n] {
			return errors.New("proposal_unbounded")
		}
	}
	return nil
}

func proposalTx(ctx context.Context, q proposalQuery, id string, rev int) (RuleProposal, error) {
	var p RuleProposal
	var b string
	err := q.QueryRowContext(ctx, `SELECT body,state,version FROM rule_proposals WHERE id=? AND revision=?`, id, rev).Scan(&b, &p.State, &p.Version)
	if err != nil {
		return p, errors.New("proposal_not_found")
	}
	var content RuleProposal
	if json.Unmarshal([]byte(b), &content) != nil {
		return p, errors.New("proposal_invalid")
	}
	content.State = p.State
	content.Version = p.Version
	return content, nil
}
func proposalAuditTx(ctx context.Context, tx *sql.Tx, p RuleProposal, kind string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO proposal_audit(proposal_id,revision,kind,created_at) VALUES(?,?,?,?)`, p.ID, p.Revision, kind, time.Now().Unix())
	return err
}
func putProposalTx(ctx context.Context, tx *sql.Tx, p RuleProposal) error {
	var count int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM rule_proposals`).Scan(&count); e != nil {
		return e
	}
	if count >= 10000 {
		return errors.New("proposal_limit")
	}
	b, _ := json.Marshal(p)
	_, err := tx.ExecContext(ctx, `INSERT INTO rule_proposals VALUES(?,?,?,'pending',1)`, p.ID, p.Revision, string(b))
	if err != nil {
		return errors.New("proposal_revision_exists")
	}
	return proposalAuditTx(ctx, tx, p, "proposal_"+p.Origin)
}

// SubmitProposal agent身份唯一写入入口；同一依据集合重复ACK返回原候选，不接受新正文。
func (s *Store) SubmitProposal(ctx context.Context, in ProposalInput) (RuleProposal, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuleProposal{}, err
	}
	defer tx.Rollback()
	key := sampleSet(in.Samples)
	id := "rp_" + key[:40]
	in.Rule.ID = id
	in.Rule.Revision = 1
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT proposal_id FROM proposal_sets WHERE set_digest=?`, key).Scan(&existing)
	if err == nil {
		p, e := proposalTx(ctx, tx, existing, 1)
		if e == nil && digestStringJSON(p.Input) != digestStringJSON(in) {
			e = errors.New("proposal_replay_conflict")
		}
		return p, e
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return RuleProposal{}, err
	}
	if err = validateProposal(ctx, tx, in); err != nil {
		return RuleProposal{}, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM rule_proposals`).Scan(&count); err != nil {
		return RuleProposal{}, err
	}
	if count >= 10000 {
		return RuleProposal{}, errors.New("proposal_limit")
	}
	p := RuleProposal{ID: id, Revision: 1, Version: 1, State: "pending", Origin: "agent", CreatedAt: time.Now().Unix(), Input: in}
	if _, err = tx.ExecContext(ctx, `INSERT INTO proposal_sets VALUES(?,?)`, key, id); err != nil {
		return p, err
	}
	if err = putProposalTx(ctx, tx, p); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

// Proposal 供本机查看不可变依据和最新状态；过期样本显式投影stale并记录审计。
func (s *Store) Proposal(ctx context.Context, id string, rev int) (RuleProposal, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuleProposal{}, err
	}
	defer tx.Rollback()
	p, err := proposalTx(ctx, tx, id, rev)
	if err != nil {
		return p, err
	}
	if p.State == "pending" {
		for _, ref := range p.Input.Samples {
			if _, e := currentSample(ctx, tx, ref); e != nil {
				p.State = "stale"
				p.Version++
				if _, err = tx.ExecContext(ctx, `UPDATE rule_proposals SET state='stale',version=version+1 WHERE id=? AND revision=? AND state='pending'`, id, rev); err != nil {
					return p, err
				}
				if err = proposalAuditTx(ctx, tx, p, "proposal_stale"); err != nil {
					return p, err
				}
				break
			}
		}
	}
	return p, tx.Commit()
}

// ProposalList 以audit序号分页，不输出模型正文到事件；每页最多20个revision。
func (s *Store) ProposalList(ctx context.Context, after int64) ([]RuleProposal, int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT min(a.seq),p.id,p.revision FROM rule_proposals p JOIN proposal_audit a ON a.proposal_id=p.id AND a.revision=p.revision GROUP BY p.id,p.revision HAVING min(a.seq)>? ORDER BY min(a.seq) LIMIT 21`, after)
	if err != nil {
		return nil, 0, err
	}
	type key struct {
		seq int64
		id  string
		rev int
	}
	keys := []key{}
	for rows.Next() {
		var k key
		if err = rows.Scan(&k.seq, &k.id, &k.rev); err != nil {
			break
		}
		keys = append(keys, k)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	next := int64(0)
	if len(keys) > 20 {
		keys = keys[:20]
		next = keys[19].seq
	}
	out := []RuleProposal{}
	for _, k := range keys {
		p, e := s.Proposal(ctx, k.id, k.rev)
		if e != nil {
			return nil, 0, e
		}
		out = append(out, p)
	}
	return out, next, nil
}

// ReviseProposal 只有本机人工可调用；旧版本转stale，新正文作为独立revision保存。
func (s *Store) ReviseProposal(ctx context.Context, id string, rev, version int, in ProposalInput) (RuleProposal, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RuleProposal{}, err
	}
	defer tx.Rollback()
	p, err := proposalTx(ctx, tx, id, rev)
	if err != nil {
		return p, err
	}
	if p.Version != version || p.State != "pending" {
		return p, errors.New("proposal_stale")
	}
	if sampleSet(p.Input.Samples) != sampleSet(in.Samples) {
		return p, errors.New("proposal_evidence_conflict")
	}
	in.Rule.ID = id
	in.Rule.Revision = rev + 1
	if err = validateProposal(ctx, tx, in); err != nil {
		return p, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE rule_proposals SET state='stale',version=version+1 WHERE id=? AND revision=?`, id, rev); err != nil {
		return p, err
	}
	if err = proposalAuditTx(ctx, tx, p, "proposal_revised"); err != nil {
		return p, err
	}
	p = RuleProposal{ID: id, Revision: rev + 1, Version: 1, State: "pending", Origin: "human", CreatedAt: time.Now().Unix(), Input: in}
	if err = putProposalTx(ctx, tx, p); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

// trialProposal 在同一事务有界试算已导出历史；明确未匹配/范围外/冲突，避免假称全历史。
func trialProposal(ctx context.Context, tx *sql.Tx, p RuleProposal) (ProposalTrial, error) {
	t := ProposalTrial{ID: p.ID, Revision: p.Revision, Version: p.Version, Coverage: []SampleRef{}, Results: []TrialResult{}, Limit: 100}
	if p.State != "pending" {
		return t, errors.New("proposal_stale")
	}
	if err := validateProposal(ctx, tx, p.Input); err != nil {
		return t, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT value FROM naming_epoch WHERE id=1`).Scan(&t.Epoch); err != nil {
		return t, err
	}
	samples := []LearningSample{}
	seen := map[string]bool{}
	for _, ref := range p.Input.Samples {
		v, err := currentSample(ctx, tx, ref)
		if err != nil {
			return t, err
		}
		samples = append(samples, v)
		seen[ref.SampleID] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT body FROM learning_samples ORDER BY seq DESC LIMIT 101`)
	if err != nil {
		return t, err
	}
	raw := []LearningSample{}
	for rows.Next() {
		var b string
		var v LearningSample
		if err = rows.Scan(&b); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(b), &v); err != nil {
			break
		}
		raw = append(raw, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return t, err
	}
	t.Truncated = len(raw) > 100
	for _, v := range raw {
		if len(samples) >= 100 {
			t.Truncated = true
			break
		}
		if seen[v.Ref.SampleID] {
			continue
		}
		if _, e := currentSample(ctx, tx, v.Ref); e != nil {
			continue
		}
		samples = append(samples, v)
		seen[v.Ref.SampleID] = true
	}
	type activeRule struct {
		rule    NamingRule
		enabled bool
	}
	active := []activeRule{}
	rows, err = tx.QueryContext(ctx, `SELECT r.body,a.enabled FROM naming_rules r JOIN naming_active a ON a.id=r.id AND a.revision=r.revision ORDER BY r.id LIMIT 1001`)
	if err != nil {
		return t, err
	}
	for rows.Next() {
		var b string
		var a activeRule
		if err = rows.Scan(&b, &a.enabled); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(b), &a.rule); err != nil {
			break
		}
		active = append(active, a)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return t, err
	}
	if len(active) > 1000 {
		return t, errors.New("proposal_trial_limit")
	}
	r := p.Input.Rule
	for _, v := range samples {
		cells := 0
		for _, c := range v.Contexts {
			if c.Subscription == r.Subscription {
				for _, change := range v.Changes {
					if videoTarget(change.Source) {
						cells++
					}
				}
			}
		}
		if len(t.Results)+cells > 2048 {
			for _, ref := range p.Input.Samples {
				if ref.SampleID == v.Ref.SampleID {
					return t, errors.New("proposal_trial_limit")
				}
			}
			t.Truncated = true
			break
		}
		if ctx.Err() != nil {
			return t, ctx.Err()
		}
		t.Coverage = append(t.Coverage, v.Ref)
		for _, change := range v.Changes {
			if !videoTarget(change.Source) {
				continue
			}
			for _, c := range v.Contexts {
				if c.Subscription != r.Subscription {
					continue
				}
				result := TrialResult{SampleID: v.Ref.SampleID, Source: change.Source, Outcome: "not_matched"}
				title, e := ParseTitle(c.Title)
				if e != nil {
					continue
				}
				if r.matches(c.Subscription, title) {
					d, e := r.Resolve(c.Title, change.Source)
					if e != nil {
						result.Outcome = "out_of_scope"
					} else {
						result.Target = d.Target
						result.Outcome = "matched"
						if d.Target != change.Target {
							result.Outcome = "conflict"
						}
						for _, a := range active {
							if a.rule.ID == r.ID || !a.rule.knownIdentity(c.Subscription, title) {
								continue
							}
							existing, e := a.rule.Resolve(c.Title, change.Source)
							if !a.enabled || e != nil || existing.Target != d.Target || existing.SeriesID != d.SeriesID || existing.Order != d.Order {
								result.Outcome = "conflict"
							}
						}
					}
				}
				if result.Outcome == "conflict" {
					t.Conflicts++
				}
				t.Results = append(t.Results, result)
			}
		}
	}
	t.Digest = digestStringJSON(t)
	return t, nil
}

// PreviewProposal 只试算，不创建或启用规则，不改媒体/计划。
func (s *Store) PreviewProposal(ctx context.Context, id string, rev, version int) (ProposalTrial, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProposalTrial{}, err
	}
	defer tx.Rollback()
	p, err := proposalTx(ctx, tx, id, rev)
	if err != nil {
		return ProposalTrial{}, err
	}
	if p.Version != version {
		return ProposalTrial{}, errors.New("proposal_stale")
	}
	return trialProposal(ctx, tx, p)
}

// DecideProposal 人工是唯一激活入口；CAS、重试幂等、样本/epoch/试算与启用共用事务。
func (s *Store) DecideProposal(ctx context.Context, id string, rev, version int, approve bool, epoch int64, digest string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := proposalTx(ctx, tx, id, rev)
	if err != nil {
		return err
	}
	state := "rejected"
	if approve {
		state = "approved"
	}
	receipt := digestStringJSON([]any{id, rev, version, approve, epoch, digest})
	if p.State == state && p.Version == version+1 {
		var prior string
		if e := tx.QueryRowContext(ctx, `SELECT request_digest FROM proposal_receipts WHERE id=? AND revision=?`, id, rev).Scan(&prior); e != nil || prior != receipt {
			return errors.New("proposal_stale")
		}
		return nil
	}
	if p.State != "pending" || p.Version != version {
		return errors.New("proposal_stale")
	}
	if approve {
		trial, e := trialProposal(ctx, tx, p)
		if e != nil {
			return e
		}
		if trial.Epoch != epoch || trial.Digest != digest || digest == "" {
			return errors.New("proposal_trial_stale")
		}
		if trial.Conflicts > 0 {
			return errors.New("proposal_conflict")
		}
		r := p.Input.Rule
		r.IdentityNormalizationVersion = identityNormalizationVersion
		if err = addRuleTx(ctx, tx, r); err != nil {
			return err
		}
		if err = setRuleTx(ctx, tx, r.ID, r.Revision, true); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE rule_proposals SET state=?,version=version+1 WHERE id=? AND revision=?`, state, id, rev); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO proposal_receipts VALUES(?,?,?)`, id, rev, receipt); err != nil {
		return err
	}
	if err = proposalAuditTx(ctx, tx, p, "proposal_"+state); err != nil {
		return err
	}
	return tx.Commit()
}
