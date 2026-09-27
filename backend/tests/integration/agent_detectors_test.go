//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/optiqor/optiqor/internal/methodology/detectors/hpa_pinned_to_min"
	"github.com/optiqor/optiqor/internal/methodology/detectors/idle_workload_observed"
	"github.com/optiqor/optiqor/internal/methodology/detectors/oomkilled_observed"
	"github.com/optiqor/optiqor/internal/methodology/detectors/orphaned_pvc"
	"github.com/optiqor/optiqor/internal/methodology/detectors/stale_namespace"
	"github.com/optiqor/optiqor/internal/methodology/detectors/vpa_divergence"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// Pins the agent-mode detectors emit one finding per stale signal +
// no findings on the well-behaved case. These run in the agent's
// in-cluster process; the SaaS path reuses them via go.mod replace
// when the agent ships a structured payload.
func TestAgentDetectors_HappyAndIdlePath(t *testing.T) {
	ctx := context.Background()
	tnt := tenancy.Context{TenantID: "tenant-int"}

	idle := idle_workload_observed.NewDetector()
	pvc := orphaned_pvc.NewDetector()
	ns := stale_namespace.NewDetector()

	idleFindings, err := idle.Analyze(ctx, tnt, []idle_workload_observed.Sample{
		{Workload: "stale", Replicas: 2, P95CPUMilli: 3, NetworkBytes7d: 100, MonthlyCostUSDCents: 10000},
		{Workload: "busy", Replicas: 3, P95CPUMilli: 700, NetworkBytes7d: 1 << 20, MonthlyCostUSDCents: 30000},
	})
	if err != nil || len(idleFindings) != 1 || idleFindings[0].Workload != "stale" {
		t.Errorf("idle: %v, findings=%+v", err, idleFindings)
	}

	pvcFindings, err := pvc.Analyze(ctx, tnt, []orphaned_pvc.PVCRef{
		{Namespace: "default", Name: "old", AgeDays: 30, MonthlyCostCents: 5000},
		{Namespace: "default", Name: "fresh", AgeDays: 2},
		{Namespace: "default", Name: "used", AgeDays: 60, Referenced: true},
	})
	if err != nil || len(pvcFindings) != 1 || pvcFindings[0].Workload != "default/old" {
		t.Errorf("pvc: %v, findings=%+v", err, pvcFindings)
	}

	nsFindings, err := ns.Analyze(ctx, tnt, []stale_namespace.Activity{
		{Namespace: "abandoned", WorkloadCount: 3, MonthlyCostUSDCents: 15000},
		{Namespace: "live", WorkloadCount: 5, CPUMilliSecondsObserved: 99, MonthlyCostUSDCents: 50000},
	})
	if err != nil || len(nsFindings) != 1 || nsFindings[0].Workload != "abandoned" {
		t.Errorf("ns: %v, findings=%+v", err, nsFindings)
	}

	oom := oomkilled_observed.NewDetector()
	oomFindings, err := oom.Analyze(ctx, tnt, []oomkilled_observed.Sample{
		{Workload: "memhog", OOMKillCount: 4, MonthlyCostUSDCents: 6000},
		{Workload: "stable", OOMKillCount: 0},
	})
	if err != nil || len(oomFindings) != 1 || oomFindings[0].Workload != "memhog" {
		t.Errorf("oom: %v, findings=%+v", err, oomFindings)
	}

	hpa := hpa_pinned_to_min.NewDetector()
	hpaFindings, err := hpa.Analyze(ctx, tnt, []hpa_pinned_to_min.Sample{
		{Workload: "api", MinReplicas: 4, MaxReplicas: 10, CurrentReplicas: 4, TargetCPUUtilPct: 70, AvgCPUUtilPct: 8, MonthlyCostUSDCents: 9000},
		{Workload: "scaling", MinReplicas: 4, MaxReplicas: 10, CurrentReplicas: 8, TargetCPUUtilPct: 70, AvgCPUUtilPct: 65},
	})
	if err != nil || len(hpaFindings) != 1 || hpaFindings[0].Workload != "api" {
		t.Errorf("hpa: %v, findings=%+v", err, hpaFindings)
	}

	vpa := vpa_divergence.NewDetector()
	vpaFindings, err := vpa.Analyze(ctx, tnt, []vpa_divergence.Sample{
		{Workload: "overprov", Mode: "Off", CurrentCPUm: 2000, RecommendCPUm: 800, MonthlyCostUSDCents: 7000},
		{Workload: "auto-vpa", Mode: "Auto", CurrentCPUm: 2000, RecommendCPUm: 500},
	})
	if err != nil || len(vpaFindings) != 1 || vpaFindings[0].Workload != "overprov" {
		t.Errorf("vpa: %v, findings=%+v", err, vpaFindings)
	}
}
