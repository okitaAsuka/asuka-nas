package nas

import (
	"context"
	"sort"
)

// bindManifest 包括不入库的辅助文件；confirm/execute均核对完整清单指纹。
func (e *Engine) bindManifest(ctx context.Context, sources []string, p *Plan) error {
	ordered := append([]string(nil), sources...)
	sort.Strings(ordered)
	fingerprints := map[string]string{}
	for _, f := range p.Files {
		fingerprints[f.Source] = f.Fingerprint
	}
	for _, source := range ordered {
		fp := fingerprints[source]
		var err error
		if fp == "" {
			fp, err = e.Files.FingerprintContext(ctx, source)
		}
		if err != nil {
			return err
		}
		p.Manifest = append(p.Manifest, SourceEvidence{source, fp})
	}
	return nil
}
