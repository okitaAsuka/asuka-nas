package nas

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

type ContextEvidence struct {
	Subscription string `json:"subscription"`
	TitleHash    string `json:"title_hash"`
}
type ManualSemantics struct {
	Identity string `json:"identity"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
}

func targetSemantics(target string) *ManualSemantics {
	stem := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	p, e := ParseTitle(stem)
	if e != nil || p.Season == 0 || p.Relative == nil || p.Relative.Start != p.Relative.End {
		return nil
	}
	loc := seasonEpisode.FindStringIndex(stem)
	if loc == nil {
		return nil
	}
	identity := strings.TrimRight(stem[:loc[0]], " ._-")
	if identity == "" {
		return nil
	}
	return &ManualSemantics{identity, p.Season, p.Relative.Start}
}
func (s *Store) snapshotEvidence(ctx context.Context, id string, p *Plan) error {
	contexts, e := s.Contexts(ctx, id)
	if e != nil {
		return e
	}
	for _, c := range contexts {
		p.Evidence = append(p.Evidence, ContextEvidence{c.Subscription, digestString(c.Title)})
	}
	return nil
}
func checkManualEvidence(p *ManualSemantics, title string) error {
	if p == nil {
		return errors.New("manual_semantics_unresolved")
	}
	t, e := ParseTitle(title)
	if e != nil {
		return e
	}
	identity, err := normalizeTitle(p.Identity)
	if err != nil {
		return err
	}
	if t.Absolute != nil || t.Collection || t.Relative == nil || t.Relative.Start != p.Episode || t.Relative.End != p.Episode || (t.Season != 0 && t.Season != p.Season) || !aliasMatched(t, identity) {
		return errors.New("manual_evidence_conflict")
	}
	return nil
}

// checkFrozenEvidence 固定规则引用优先；新增证据不重新采用未来规则的目录/集序。
func (s *Store) checkFrozenEvidence(ctx context.Context, j *Job) error {
	p := j.Plan
	if p == nil || len(p.Files) == 0 {
		return errors.New("invalid_plan")
	}
	if len(p.RuleRefs) > 0 {
		if p.Collection {
			for source, d := range p.Decisions {
				got, _, e := s.resolveNaming(ctx, j.ID, source, p.RuleRefs)
				if e != nil {
					return e
				}
				if got == nil || *got != d {
					return errors.New("mapping_conflict")
				}
			}
			return nil
		}
		d, _, e := s.resolveNaming(ctx, j.ID, p.Files[0].Source, p.RuleRefs)
		if e != nil {
			return e
		}
		if d == nil || p.Decision == nil || *d != *p.Decision {
			return errors.New("mapping_conflict")
		}
		return nil
	}
	manual, evidence := p.Manual, p.Evidence
	if p.Batch == nil && manual == nil && len(evidence) == 0 {
		var b string
		err := s.db.QueryRowContext(ctx, "SELECT body FROM legacy_plan_evidence WHERE job_id=? AND plan_version=?", j.ID, p.Version).Scan(&b)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("legacy_evidence_unresolved")
			}
			return err
		}
		var proof legacyEvidence
		if err = json.Unmarshal([]byte(b), &proof); err != nil {
			return err
		}
		manual, evidence = proof.Manual, proof.Evidence
	}
	contexts, e := s.Contexts(ctx, j.ID)
	if e != nil {
		return e
	}
	known := map[ContextEvidence]bool{}
	subs := map[string]bool{}
	for _, c := range evidence {
		known[c] = true
		subs[c.Subscription] = true
	}
	for _, c := range contexts {
		if known[ContextEvidence{c.Subscription, digestString(c.Title)}] {
			continue
		}
		if p.Batch != nil || !subs[c.Subscription] {
			return errors.New("source_mapping_unresolved")
		}
		if e = checkManualEvidence(manual, c.Title); e != nil {
			return e
		}
	}
	return nil
}
