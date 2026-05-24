//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/optiqor/optiqor/internal/agent/llm/audit"
	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestLLMAudit_RoundtripPreservesHashes(t *testing.T) {
	a := audit.NewInMemoryAuditor()
	ctx := context.Background()
	tc := tenancy.Context{TenantID: "tenant-llm-1"}

	in := "EXPLANATION:\nfix CPU\nDIFF:\n--- a\n+++ b\n@@ -1 +1 @@\n-cpu: 2\n+cpu: 1\n"
	out := "applied diff -> validators clean"
	rec := audit.Record{
		ID:           "rec-1",
		Workload:     "api",
		Model:        "claude-sonnet",
		InputSHA256:  audit.Hash(in),
		OutputSHA256: audit.Hash(out),
		InputBytes:   len(in),
		OutputBytes:  len(out),
		CostUSDCents: 5,
	}
	if err := a.Record(ctx, tc, rec); err != nil {
		t.Fatalf("Record: %v", err)
	}
	snap := a.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("Snapshot len = %d, want 1", len(snap))
	}
	if snap[0].InputSHA256 != audit.Hash(in) {
		t.Errorf("input hash drift")
	}
	if snap[0].OutputSHA256 != audit.Hash(out) {
		t.Errorf("output hash drift")
	}
}

func TestLLMAudit_HashIsCollisionResistantOnSmallInputs(t *testing.T) {
	a := audit.Hash("cpu: 1")
	b := audit.Hash("cpu: 2")
	if a == b {
		t.Errorf("Hash collision on near-identical inputs: %q == %q", a, b)
	}
}
