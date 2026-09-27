//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/applyfix/gate"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/worker"
	"github.com/optiqor/optiqor/internal/worker/workflows"
)

func TestApplyFix_EndToEnd_GateAllowsSafeDiff(t *testing.T) {
	chart := strings.Join([]string{
		"api:",
		"  resources:",
		"    requests:",
		"      cpu: \"2\"",
		"      memory: 4Gi",
		"    limits:",
		"      cpu: \"4\"",
		"      memory: 8Gi",
		"",
	}, "\n")
	safeDiff := strings.Join([]string{
		"EXPLANATION:",
		"trim CPU request from 2 to 1.5; observed P95 < 1.2 vCPU over 30d",
		"",
		"DIFF:",
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -3,3 +3,3 @@",
		"     requests:",
		"-      cpu: \"2\"",
		"+      cpu: \"1500m\"",
		"       memory: 4Gi",
	}, "\n")

	pipeline := gate.NewPipeline(gate.SkeletonPolicy{},
		gate.RenderValidator{},
		gate.NotImplementedValidator{S: gate.StageConform},
		gate.NotImplementedValidator{S: gate.StageDryrun},
		gate.PostValidator{MaxResourceReductionRatio: 0.5},
	)
	pub := &capturePublisher{}

	wf := workflows.ApplyFix{
		Composer: &agent.Composer{
			LLM: &agent.FakeLLMClient{Responses: []agent.LLMResponse{{Text: safeDiff, Model: "claude-sonnet"}}},
		},
		Gate:      pipeline,
		Publisher: pub,
	}

	payload := mustJSON(t, workflows.ApplyFixPayload{
		RepoOwner: "acme", RepoName: "api", BaseBranch: "main", ChartPath: "charts/api",
		ChartYAML: chart,
		Model:     "claude-sonnet",
		Finding: rules.Finding{
			DetectorID: "cpu-overprovisioned", Workload: "api",
			Title:    "CPU overprovisioned",
			Severity: rules.SeverityMed, Category: rules.CategoryCost,
		},
		ApplyFixID: "afix_int_1",
		Now:        time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
	})

	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "tenant-1"}, payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if pub.calls != 1 {
		t.Errorf("Publisher called %d times, want 1", pub.calls)
	}
	if pub.last.Title == "" || pub.last.UnifiedDiff == "" {
		t.Errorf("Publisher got incomplete PR: %+v", pub.last)
	}
	if !strings.Contains(pub.last.UnifiedDiff, "cpu: \"1500m\"") {
		t.Errorf("diff body missing replacement: %q", pub.last.UnifiedDiff)
	}
}

func TestApplyFix_EndToEnd_GateRejectsUnsafeDiff(t *testing.T) {
	chart := strings.Join([]string{
		"api:",
		"  resources:",
		"    requests:",
		"      cpu: \"2\"",
		"      memory: 4Gi",
		"",
	}, "\n")
	// A 95% CPU cut — the PostValidator's safety floor (50% default)
	// must reject this before Publisher is called.
	unsafeDiff := strings.Join([]string{
		"EXPLANATION:",
		"aggressive cut for testing",
		"",
		"DIFF:",
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -3,3 +3,3 @@",
		"     requests:",
		"-      cpu: \"2\"",
		"+      cpu: \"100m\"",
		"       memory: 4Gi",
	}, "\n")

	pipeline := gate.NewPipeline(gate.SkeletonPolicy{},
		gate.RenderValidator{},
		gate.NotImplementedValidator{S: gate.StageConform},
		gate.NotImplementedValidator{S: gate.StageDryrun},
		gate.PostValidator{MaxResourceReductionRatio: 0.5},
	)
	pub := &capturePublisher{}

	wf := workflows.ApplyFix{
		Composer: &agent.Composer{
			LLM: &agent.FakeLLMClient{Responses: []agent.LLMResponse{{Text: unsafeDiff, Model: "claude-sonnet"}}},
		},
		Gate:      pipeline,
		Publisher: pub,
	}

	payload := mustJSON(t, workflows.ApplyFixPayload{
		RepoOwner: "acme", RepoName: "api", BaseBranch: "main", ChartPath: "charts/api",
		ChartYAML: chart,
		Model:     "claude-sonnet",
		Finding: rules.Finding{
			DetectorID: "cpu-overprovisioned", Workload: "api",
			Title:    "CPU overprovisioned",
			Severity: rules.SeverityMed, Category: rules.CategoryCost,
		},
		ApplyFixID: "afix_int_2",
		Now:        time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
	})

	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "tenant-1"}, payload)
	if err == nil {
		t.Fatal("expected gate to reject; got nil")
	}
	if !strings.Contains(err.Error(), "gate") {
		t.Errorf("err %q should name the gate", err.Error())
	}
	if pub.calls != 0 {
		t.Errorf("Publisher called %d times, want 0 (gate must fail-closed)", pub.calls)
	}
}

func TestApplyFix_AllWorkflows_RegisterableInDispatcher(t *testing.T) {
	disp := worker.NewInMemory()
	pipeline := gate.NewPipeline(gate.SkeletonPolicy{}, gate.SkeletonValidators()...)
	if err := disp.Register(workflows.ApplyFix{
		Composer:  &agent.Composer{LLM: &agent.FakeLLMClient{}},
		Gate:      pipeline,
		Publisher: &capturePublisher{},
	}); err != nil {
		t.Fatalf("register apply_fix: %v", err)
	}
	if len(disp.Workflows()) != 1 {
		t.Errorf("dispatcher Workflows = %d, want 1", len(disp.Workflows()))
	}
}

type capturePublisher struct {
	mu    sync.Mutex
	calls int
	last  workflows.PullRequest
	err   error
}

func (p *capturePublisher) Publish(_ context.Context, _ tenancy.Context, pr workflows.PullRequest) (workflows.PRResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return workflows.PRResult{}, p.err
	}
	p.calls++
	p.last = pr
	return workflows.PRResult{URL: "https://github.com/acme/api/pull/1", Number: 1}, nil
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var _ = errors.Is // keep errors package used if we extend later
