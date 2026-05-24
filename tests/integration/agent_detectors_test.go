//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/optiqor/optiqor/internal/methodology/detectors/idle_workload_observed"
	"github.com/optiqor/optiqor/internal/methodology/detectors/orphaned_pvc"
	"github.com/optiqor/optiqor/internal/methodology/detectors/stale_namespace"
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
}
