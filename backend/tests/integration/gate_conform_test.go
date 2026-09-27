//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/applyfix/gate"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// End-to-end check on a full Pipeline including ConformValidator —
// pinning that the real Phase-4 gate composition behaves correctly
// (render → conform → post) under both safe + unsafe inputs.
func TestGate_RealPipeline_AcceptsAndRejects(t *testing.T) {
	chart := strings.Join([]string{
		"api:",
		"  image: nginx:1.27",
		"  resources:",
		"    requests:",
		"      cpu: \"2\"",
		"      memory: 4Gi",
		"",
	}, "\n")

	safe := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -4,3 +4,3 @@",
		"     requests:",
		"-      cpu: \"2\"",
		"+      cpu: \"1500m\"",
		"       memory: 4Gi",
	}, "\n")

	imageDrop := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -2,3 +2,2 @@",
		" api:",
		"-  image: nginx:1.27",
		"   resources:",
	}, "\n")

	unsafeCut := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -4,3 +4,3 @@",
		"     requests:",
		"-      cpu: \"2\"",
		"+      cpu: \"100m\"",
		"       memory: 4Gi",
	}, "\n")

	pipeline := gate.NewPipeline(gate.SkeletonPolicy{},
		gate.RenderValidator{},
		gate.ConformValidator{RequiredKeys: []string{"image", "resources"}},
		gate.NotImplementedValidator{S: gate.StageDryrun},
		gate.PostValidator{MaxResourceReductionRatio: 0.5},
	)
	ctx := context.Background()
	tnt := tenancy.Context{TenantID: "t-1"}

	for _, tc := range []struct {
		name     string
		cand     gate.Candidate
		wantErr  bool
		errStage gate.Stage
	}{
		{name: "safe diff passes every stage", cand: gate.Candidate{ChartYAML: chart, UnifiedDiff: safe}},
		{name: "image-removal blocked by conform", cand: gate.Candidate{ChartYAML: chart, UnifiedDiff: imageDrop}, wantErr: true, errStage: gate.StageConform},
		{name: "aggressive cut blocked by post", cand: gate.Candidate{ChartYAML: chart, UnifiedDiff: unsafeCut}, wantErr: true, errStage: gate.StagePost},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pipeline.Run(ctx, tnt, tc.cand)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected gate to fail")
				}
				if !strings.Contains(err.Error(), string(tc.errStage)) {
					t.Errorf("err %q should name stage %q", err.Error(), tc.errStage)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected gate failure: %v", err)
			}
		})
	}
}
