package nas

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// 同一证据合同覆盖规则合集与人工 batch；不依赖 helper 的位置或源码文本。
func manifestPlanner(t *testing.T, mode string) (*fixture, string, func(context.Context, Job) (*Plan, error)) {
	t.Helper()
	if mode == "collection" {
		f := packFixture(t, "mixed")
		return f, "fixture.txt", func(ctx context.Context, j Job) (*Plan, error) {
			return f.engine.collectionPlan(ctx, &j)
		}
	}
	f, batch := batchFixture(t)
	return f, "notes.txt", func(ctx context.Context, j Job) (*Plan, error) {
		return f.engine.batchPlan(ctx, j, batch)
	}
}

func TestPlanManifestEvidenceContract(t *testing.T) {
	for _, mode := range []string{"collection", "batch"} {
		t.Run(mode, func(t *testing.T) {
			f, _, build := manifestPlanner(t, mode)
			ctx := context.Background()
			job := f.job()
			job.Sources = slices.Clone(f.manifest)
			original := slices.Clone(job.Sources)
			before, err := f.store.Events(ctx, 0)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := build(ctx, job)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(job.Sources, original) {
				t.Fatal("preview reordered downloader manifest")
			}
			ordered := slices.Clone(original)
			slices.Sort(ordered)
			want := make([]SourceEvidence, 0, len(ordered))
			for _, source := range ordered {
				fp, err := f.files.FingerprintContext(ctx, source)
				if err != nil {
					t.Fatal(err)
				}
				want = append(want, SourceEvidence{source, fp})
			}
			if !reflect.DeepEqual(plan.Manifest, want) || len(plan.Manifest) <= len(plan.Files) {
				t.Fatal("manifest must include sorted, real fingerprints for imported and auxiliary files")
			}
			slices.Reverse(job.Sources)
			reordered, err := build(ctx, job)
			if err != nil {
				t.Fatal(err)
			}
			a, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(reordered)
			if err != nil || string(a) != string(b) || PlanDigest(plan) != PlanDigest(reordered) {
				t.Fatal("equivalent manifest order changed frozen plan bytes or digest", err)
			}
			after, err := f.store.Events(ctx, 0)
			if err != nil || !reflect.DeepEqual(before, after) || f.job().Version != job.Version {
				t.Fatal("preview persisted state or events", err)
			}
			entries, err := os.ReadDir(f.library)
			if err != nil || len(entries) != 0 {
				t.Fatal("preview created library entries", err)
			}
		})
	}
}

func TestPlanManifestRejectsUnavailableAuxiliary(t *testing.T) {
	for _, mode := range []string{"collection", "batch"} {
		for _, failure := range []string{"missing", "symlink", "cancelled"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				f, auxiliary, build := manifestPlanner(t, mode)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				job := f.job()
				job.Sources = slices.Clone(f.manifest)
				if failure == "cancelled" {
					cancel()
				} else {
					p := filepath.Join(f.downloads, auxiliary)
					if err := os.Remove(p); err != nil {
						t.Fatal(err)
					}
					if failure == "symlink" {
						outside := filepath.Join(f.root, "outside.txt")
						if err := os.WriteFile(outside, []byte("bounded fixture"), 0600); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(outside, p); err != nil {
							t.Fatal(err)
						}
					}
				}
				plan, err := build(ctx, job)
				if err == nil || plan != nil {
					t.Fatal("failed evidence returned a usable plan", err)
				}
				if failure != "cancelled" && err.Error() != "unsafe_or_missing_path" {
					t.Fatal("unexpected auxiliary rejection", err)
				}
				if f.job().Version != job.Version || f.job().Plan != nil {
					t.Fatal("failed preview persisted a plan")
				}
				entries, err := os.ReadDir(f.library)
				if err != nil || len(entries) != 0 {
					t.Fatal("failed preview created library entries", err)
				}
			})
		}
	}
}
