//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/applyfix/gate"
	"github.com/optiqor/optiqor/internal/operators"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/validator"
	"github.com/optiqor/optiqor/internal/worker/workflows"
)

func TestApplyFix_ValidatorRejectsCandidate_BlocksPublish(t *testing.T) {
	chart := strings.Join([]string{
		"api:",
		"  resources:",
		"    requests:",
		"      cpu: \"2\"",
		"      memory: 4Gi",
		"",
	}, "\n")
	diff := strings.Join([]string{
		"EXPLANATION:",
		"cut replicas",
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

	pipeline := gate.NewPipeline(gate.SkeletonPolicy{}, gate.RenderValidator{}, gate.PostValidator{MaxResourceReductionRatio: 0.5})
	pub := &capturePublisher{}

	wf := workflows.ApplyFix{
		Composer:  &agent.Composer{LLM: &agent.FakeLLMClient{Responses: []agent.LLMResponse{{Text: diff, Model: "claude-sonnet"}}}},
		Gate:      pipeline,
		Validator: validator.NewPipeline(validator.PDBCheck{}, validator.OOMRecentCheck{}),
		Publisher: pub,
	}

	// Recent OOMKill + memory cut → validator rejects before Publish.
	payload, _ := json.Marshal(workflows.ApplyFixPayload{
		RepoOwner: "acme", RepoName: "api", BaseBranch: "main", ChartPath: "charts/api",
		ChartYAML: chart, Model: "claude-sonnet",
		Finding: rules.Finding{
			DetectorID: "memory-overprovisioned", Workload: "api",
			Title:    "Memory overprovisioned",
			Severity: rules.SeverityMed, Category: rules.CategoryCost,
		},
		ApplyFixID:      "afix_v_1",
		Now:             time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		ClusterSignals:  validator.ClusterSignals{OOMRecent: true},
		ProposedMemoryB: 2 << 30, // 2 GiB
	})

	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "tenant-int"}, payload)
	if err == nil {
		t.Fatal("expected validator rejection")
	}
	if !strings.Contains(err.Error(), "OOMKill") {
		t.Errorf("err %q should name the oom-recent rejection", err.Error())
	}
	if pub.calls != 0 {
		t.Errorf("Publisher called %d times, want 0 when validator rejects", pub.calls)
	}
}

func TestApplyFix_OperatorOwnedWorkload_Rejected(t *testing.T) {
	chart := "api:\n  resources:\n    requests:\n      cpu: \"2\"\n      memory: 4Gi\n"
	diff := strings.Join([]string{
		"EXPLANATION:",
		"trim",
		"",
		"DIFF:",
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -1,3 +1,3 @@",
		" api:",
		"   resources:",
		"     requests:",
	}, "\n")

	pipeline := gate.NewPipeline(gate.SkeletonPolicy{}, gate.RenderValidator{})
	pub := &capturePublisher{}

	wf := workflows.ApplyFix{
		Composer:  &agent.Composer{LLM: &agent.FakeLLMClient{Responses: []agent.LLMResponse{{Text: diff, Model: "claude-sonnet"}}}},
		Gate:      pipeline,
		Publisher: pub,
		OwnerResolve: func(ref operators.OwnerRef) (operators.OwnerRef, bool) {
			// First owner already names a CRD; return false to terminate
			// the walk so the controlling owner stays operator-owned.
			return operators.OwnerRef{}, false
		},
	}

	payload, _ := json.Marshal(workflows.ApplyFixPayload{
		RepoOwner: "acme", RepoName: "api", BaseBranch: "main", ChartPath: "charts/api",
		ChartYAML: chart, Model: "claude-sonnet",
		Finding: rules.Finding{
			DetectorID: "cpu-overprovisioned", Workload: "api",
			Title:    "CPU overprovisioned",
			Severity: rules.SeverityMed, Category: rules.CategoryCost,
		},
		ApplyFixID: "afix_op_1",
		Now:        time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC),
		WorkloadOwners: []operators.OwnerRef{{
			APIVersion: "kafka.strimzi.io/v1beta2",
			Kind:       "Kafka",
			Name:       "events-kafka",
			Controller: true,
		}},
	})

	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "tenant-int"}, payload)
	if err == nil {
		t.Fatal("expected operator-owned workload to be rejected")
	}
	if !strings.Contains(err.Error(), "operator-owned") {
		t.Errorf("err %q should name the operator gate", err.Error())
	}
	if pub.calls != 0 {
		t.Errorf("Publisher called %d times, want 0", pub.calls)
	}
}
