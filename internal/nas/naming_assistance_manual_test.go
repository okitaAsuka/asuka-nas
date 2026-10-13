package nas

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// 普通人工批量授权不依赖模型；有辅助历史即保守标来源，不把未知结果当纯人工训练依据。
func TestNamingManualBatchFallbackWithoutModelRetry(t *testing.T) {
	for _, name := range []string{"rejected", "stale_context", "stale_fingerprint", "offline", "stale_rule"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := namingFixture(t)
			v := namingRequest(t, f)
			if name != "offline" {
				if _, err := f.engine.SubmitNaming(ctx, namingResult(v)); err != nil {
					t.Fatal(err)
				}
			}
			switch name {
			case "rejected":
				if err := f.engine.RejectNaming(ctx, v.ID, 2); err != nil {
					t.Fatal(err)
				}
			case "stale_context":
				if err := f.store.AttachResourceTitle(ctx, "fixture", "private-guid", "Example changed [01]"); err != nil {
					t.Fatal(err)
				}
			case "stale_fingerprint":
				if err := os.WriteFile(filepath.Join(f.downloads, f.name), []byte("fresh current content"), 0600); err != nil {
					t.Fatal(err)
				}
			case "stale_rule":
				installRule(t, f.store, manualRule())
			}
			saved, _, err := f.store.namingReview(ctx, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			original := digestStringJSON(saved)
			b := BatchOrganization{Directory: "Human reviewed", DisplayName: "Example", Season: 3, Mode: "manual", Rows: []BatchRow{{f.name, 1}}}
			preview, err := f.engine.BatchPreview(ctx, f.job().ID, f.job().Version, b)
			if err != nil || preview.Plan.Assistance == nil || preview.Plan.NamingReview {
				t.Fatal("manual fallback preview blocked", err)
			}
			if name == "offline" && preview.Plan.Assistance.ResultDigest != "" {
				t.Fatal("fabricated missing model result")
			}
			// 审核期间源再次变化仍应拒绝：恢复当前内容后重新人工preview，而不是重新模型请求。
			if name == "stale_fingerprint" {
				if err = os.WriteFile(filepath.Join(f.downloads, f.name), []byte("changed after human preview"), 0600); err != nil {
					t.Fatal(err)
				}
				if err = f.engine.BatchConfirm(ctx, f.job().ID, preview.JobVersion, preview.Plan.Version, preview.Digest, b); err == nil {
					t.Fatal("manual bypassed current fingerprint")
				}
				preview, err = f.engine.BatchPreview(ctx, f.job().ID, f.job().Version, b)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = f.engine.BatchConfirm(ctx, f.job().ID, preview.JobVersion, preview.Plan.Version, preview.Digest, b); err != nil {
				t.Fatal("manual confirmation blocked by model state", err)
			}
			f.mu.Lock()
			f.discovered = true
			f.mu.Unlock()
			f.tick(1001)
			j := f.job()
			if j.Import != "imported" || j.Discovery != "confirmed" {
				t.Fatal("manual did not execute")
			}
			src, _ := os.Stat(filepath.Join(f.downloads, f.name))
			dst, err := os.Stat(filepath.Join(f.library, j.Plan.Files[0].Target))
			if err != nil || !os.SameFile(src, dst) {
				t.Fatal("not hardlink")
			}
			after, _, err := f.store.namingReview(ctx, v.ID)
			if err != nil || digestStringJSON(after) != original || requestCount(t, f) != 1 {
				t.Fatal("manual fallback rewrote/re-requested suggestion")
			}
			records := comparisons(t, f)
			if len(records) != 1 || records[0].Intent.Origin != "model_assisted_human" || records[0].Intent.Assistance == nil {
				t.Fatal("human evidence laundering")
			}
			page, err := f.store.LearningSamples(ctx, 0)
			if err != nil || len(page.Items) != 0 {
				t.Fatal("manual fallback entered pure-human learning")
			}
		})
	}
}
