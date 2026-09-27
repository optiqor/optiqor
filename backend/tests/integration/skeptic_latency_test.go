//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/applyfix/gate"
	"github.com/optiqor/optiqor/internal/applyfix/latency"
	"github.com/optiqor/optiqor/internal/platform/telemetry"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/validator"
	"github.com/optiqor/optiqor/internal/worker/workflows"
)

// TestApplyFix_LatencyHistogramsRecordEveryStep pins the SLO contract:
// every Apply-Fix dispatch increments every step's histogram. Catches
// a future regression where a step is bypassed silently.
func TestApplyFix_LatencyHistogramsRecordEveryStep(t *testing.T) {
	chart := strings.Join([]string{"api:", "  resources:", "    requests:", "      cpu: \"2\"", "      memory: 4Gi", ""}, "\n")
	diff := strings.Join([]string{
		"EXPLANATION:", "trim", "",
		"DIFF:",
		"--- a/values.yaml", "+++ b/values.yaml",
		"@@ -3,3 +3,3 @@",
		"     requests:",
		"-      cpu: \"2\"",
		"+      cpu: \"1500m\"",
		"       memory: 4Gi",
	}, "\n")

	reg := telemetry.NewRegistry()
	rec := latency.NewRecorder(reg)
	pipeline := gate.NewPipeline(gate.SkeletonPolicy{}, gate.RenderValidator{}, gate.PostValidator{MaxResourceReductionRatio: 0.5})

	wf := workflows.ApplyFix{
		Composer:  &agent.Composer{LLM: &agent.FakeLLMClient{Responses: []agent.LLMResponse{{Text: diff, Model: "claude-sonnet"}}}},
		Gate:      pipeline,
		Publisher: &noopPublisher{},
		Latency:   rec,
	}

	payload, _ := json.Marshal(workflows.ApplyFixPayload{
		RepoOwner: "acme", RepoName: "api", BaseBranch: "main", ChartPath: "charts/api",
		ChartYAML: chart, Model: "claude-sonnet",
		Finding: rules.Finding{
			DetectorID: "cpu-overprovisioned", Workload: "api",
			Title: "CPU overprovisioned", Severity: rules.SeverityMed, Category: rules.CategoryCost,
		},
		ApplyFixID: "afix_lat_1",
		Now:        time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
	})

	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "tenant-lat"}, payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Mandatory steps for the happy path (no operator gate because no
	// WorkloadOwners; no validator because Validator is nil).
	for _, step := range []latency.Step{
		latency.StepParse, latency.StepCompose, latency.StepGate, latency.StepRender, latency.StepPublish, latency.StepTotal,
	} {
		var buf strings.Builder
		_ = reg.WriteText(&buf)
		if !strings.Contains(buf.String(), `step="`+string(step)+`"`) {
			t.Errorf("histogram for step %q missing from exposition:\n%s", step, buf.String())
		}
	}
}

// TestApplyFix_SkepticMode_RejectsNotImplementedStage pins the
// Phase-4 fail-closed contract: with SkepticMode on, the dryrun
// stage's NotImplemented status hard-stops dispatch.
func TestApplyFix_SkepticMode_RejectsNotImplementedStage(t *testing.T) {
	chart := strings.Join([]string{"api:", "  resources:", "    requests:", "      cpu: \"2\"", "      memory: 4Gi", ""}, "\n")
	diff := strings.Join([]string{
		"EXPLANATION:", "trim", "",
		"DIFF:",
		"--- a/values.yaml", "+++ b/values.yaml",
		"@@ -3,3 +3,3 @@",
		"     requests:",
		"-      cpu: \"2\"",
		"+      cpu: \"1500m\"",
		"       memory: 4Gi",
	}, "\n")

	// SkeletonValidators includes a Dryrun NotImplemented stub.
	pipeline := gate.NewPipeline(gate.SkeletonPolicy{}, gate.SkeletonValidators()...)
	pub := &noopPublisher{}

	wf := workflows.ApplyFix{
		Composer:    &agent.Composer{LLM: &agent.FakeLLMClient{Responses: []agent.LLMResponse{{Text: diff, Model: "claude-sonnet"}}}},
		Gate:        pipeline,
		Validator:   validator.NewPipeline(validator.PDBCheck{}),
		Publisher:   pub,
		SkepticMode: true,
	}

	payload, _ := json.Marshal(workflows.ApplyFixPayload{
		RepoOwner: "acme", RepoName: "api", BaseBranch: "main", ChartPath: "charts/api",
		ChartYAML: chart, Model: "claude-sonnet",
		Finding: rules.Finding{
			DetectorID: "cpu-overprovisioned", Workload: "api",
			Title: "CPU overprovisioned", Severity: rules.SeverityMed, Category: rules.CategoryCost,
		},
		ApplyFixID: "afix_sk_1",
		Now:        time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
	})

	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "tenant-sk"}, payload)
	if err == nil {
		t.Fatal("expected skeptic mode rejection")
	}
	if !strings.Contains(err.Error(), "skeptic") {
		t.Errorf("err %q should name skeptic mode", err.Error())
	}
	if pub.calls != 0 {
		t.Errorf("Publisher called %d times, want 0", pub.calls)
	}
}

// TestApplyFix_SkepticMode_RequiresValidator pins the boot-time
// fail-closed: SkepticMode + nil Validator must error before any
// state changes.
func TestApplyFix_SkepticMode_RequiresValidator(t *testing.T) {
	wf := workflows.ApplyFix{
		Composer:    &agent.Composer{LLM: &agent.FakeLLMClient{}},
		Gate:        gate.NewPipeline(gate.SkeletonPolicy{}, gate.RenderValidator{}),
		Publisher:   &noopPublisher{},
		SkepticMode: true,
		// Validator intentionally nil.
	}
	payload, _ := json.Marshal(workflows.ApplyFixPayload{
		ChartYAML: "x: 1\n",
		Finding:   rules.Finding{DetectorID: "x", Workload: "x"},
	})
	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t"}, payload)
	if err == nil || !strings.Contains(err.Error(), "skeptic mode requires") {
		t.Errorf("err = %v, want substring 'skeptic mode requires'", err)
	}
}

type noopPublisher struct {
	mu    sync.Mutex
	calls int
}

func (p *noopPublisher) Publish(_ context.Context, _ tenancy.Context, _ workflows.PullRequest) (workflows.PRResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return workflows.PRResult{URL: "https://x/1", Number: 1}, nil
}
