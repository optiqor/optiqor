package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/agent/provisioner"
	"github.com/optiqor/optiqor/internal/safety/environment"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/validator"
)

func TestApplyFix_StrategyGate_RejectsLowConfidenceInProd(t *testing.T) {
	payload, _ := json.Marshal(ApplyFixPayload{
		ChartYAML:        "api:\n  resources: {requests: {cpu: 2}}",
		Finding:          rules.Finding{Workload: "api", Title: "x", Confidence: rules.ConfidenceLow},
		Environment:      environment.EnvProd,
		ProvisionerClass: provisioner.ClassKarpenter,
		ApplyFixID:       "afix_1",
	})
	wf := ApplyFix{
		Composer:  &agent.Composer{LLM: &agent.FakeLLMClient{}},
		Gate:      newPassPipeline(t),
		Publisher: &fakePublisher{},
	}
	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload)
	if err == nil {
		t.Fatal("expected rejection on low confidence in prod")
	}
}

func TestApplyFix_StrategyGate_AllowsMatchingConfidence(t *testing.T) {
	payload, _ := json.Marshal(ApplyFixPayload{
		ChartYAML: "api:\n  resources: {requests: {cpu: 2}}",
		Finding: rules.Finding{
			Workload:   "api",
			Title:      "x",
			Confidence: rules.ConfidenceHigh,
			Severity:   rules.SeverityMed,
			Category:   rules.CategoryCost,
		},
		Environment:      environment.EnvProd,
		ProvisionerClass: provisioner.ClassKarpenter,
		ApplyFixID:       "afix_1",
	})
	wf := ApplyFix{
		Composer: &agent.Composer{LLM: &agent.FakeLLMClient{Responses: []agent.LLMResponse{{
			Text:  "EXPLANATION:\nfix\nDIFF:\n--- a\n+++ b\n@@\n- cpu: 2\n+ cpu: 1\n",
			Model: "claude-sonnet",
		}}}},
		Gate:      newPassPipeline(t),
		Publisher: &fakePublisher{},
	}
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestApplyFix_StrategyGate_StaticCapsReplicaCuts(t *testing.T) {
	payload, _ := json.Marshal(ApplyFixPayload{
		ChartYAML: "api:\n  resources: {requests: {cpu: 2}}",
		Finding: rules.Finding{
			Workload:   "api",
			Title:      "x",
			Confidence: rules.ConfidenceHigh,
			Severity:   rules.SeverityMed,
		},
		Environment:      environment.EnvStaging,
		ProvisionerClass: provisioner.ClassStatic,
		ProposedReplicas: 1,
		ClusterSignals: validator.ClusterSignals{
			HPA: &validator.HPA{MinReplicas: 1, MaxReplicas: 10, CurrentReps: 5},
		},
		ApplyFixID: "afix_1",
	})
	wf := ApplyFix{
		Composer:  &agent.Composer{LLM: &agent.FakeLLMClient{}},
		Gate:      newPassPipeline(t),
		Publisher: &fakePublisher{},
	}
	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload)
	if err == nil {
		t.Fatal("expected rejection: static class zeros replica cap")
	}
}

type stubSettings struct {
	value bool
	err   error
}

func (s stubSettings) SkepticModeDefault(_ context.Context, _ tenancy.Context) (bool, error) {
	return s.value, s.err
}

func TestApplyFix_TenantSettings_OverridesWorkerDefault(t *testing.T) {
	payload, _ := json.Marshal(ApplyFixPayload{
		ChartYAML:  "api:\n  resources: {requests: {cpu: 2}}",
		Finding:    rules.Finding{Workload: "api", Title: "x", Severity: rules.SeverityMed, Category: rules.CategoryCost},
		ApplyFixID: "afix_1",
	})
	wf := ApplyFix{
		Composer: &agent.Composer{LLM: &agent.FakeLLMClient{Responses: []agent.LLMResponse{{
			Text:  "EXPLANATION:\nfix\nDIFF:\n--- a\n+++ b\n@@\n- cpu: 2\n+ cpu: 1\n",
			Model: "claude-sonnet",
		}}}},
		Gate:           newPassPipeline(t),
		Publisher:      &fakePublisher{},
		Validator:      validator.NewPipeline(validator.Default()...),
		SkepticMode:    true,
		TenantSettings: stubSettings{value: false},
	}
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload); err != nil {
		t.Fatalf("Execute: %v — tenant override to false should drop skeptic", err)
	}
}

func TestApplyFix_TenantSettings_FailureFallsBackToWorker(t *testing.T) {
	// Source errors. Workflow falls back to worker SkepticMode = true,
	// so a nil validator wiring becomes an error at boot.
	payload, _ := json.Marshal(ApplyFixPayload{
		ChartYAML:  "api:\n  resources: {requests: {cpu: 2}}",
		Finding:    rules.Finding{Workload: "api", Title: "x"},
		ApplyFixID: "afix_1",
	})
	wf := ApplyFix{
		Composer:       &agent.Composer{LLM: &agent.FakeLLMClient{}},
		Gate:           newPassPipeline(t),
		Publisher:      &fakePublisher{},
		SkepticMode:    true,
		TenantSettings: stubSettings{err: errors.New("db down")},
	}
	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload)
	if err == nil || err.Error() != "apply_fix: skeptic mode requires a Validator" {
		t.Fatalf("want skeptic-fallback error, got %v", err)
	}
}
