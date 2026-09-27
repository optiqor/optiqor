package idle_workload_observed

import (
	"context"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestDetector_Analyze(t *testing.T) {
	ctx := context.Background()
	tnt := tenancy.Context{TenantID: "t"}
	d := NewDetector()

	for _, tc := range []struct {
		name    string
		samples []Sample
		wantIDs []string
	}{
		{
			name: "idle workload fires",
			samples: []Sample{
				{Workload: "stale-api", Replicas: 3, P95CPUMilli: 5, NetworkBytes7d: 100, MonthlyCostUSDCents: 30000},
			},
			wantIDs: []string{"stale-api"},
		},
		{
			name: "active workload skipped",
			samples: []Sample{
				{Workload: "busy", Replicas: 3, P95CPUMilli: 800, NetworkBytes7d: 1 << 20, MonthlyCostUSDCents: 30000},
			},
		},
		{
			name: "zero replicas skipped (CLI sandbox covers it)",
			samples: []Sample{
				{Workload: "scaled-zero", Replicas: 0, P95CPUMilli: 0, NetworkBytes7d: 0},
			},
		},
		{
			name: "HPA + measurable CPU skipped",
			samples: []Sample{
				{Workload: "hpa-min", Replicas: 1, HasHPA: true, P95CPUMilli: 5, NetworkBytes7d: 0, MonthlyCostUSDCents: 1000},
			},
		},
		{
			name: "noisy network barely over threshold skipped",
			samples: []Sample{
				{Workload: "almost-idle", Replicas: 2, P95CPUMilli: 5, NetworkBytes7d: 2 << 20, MonthlyCostUSDCents: 1000},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := d.Analyze(ctx, tnt, tc.samples)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("findings = %d (%+v), want %d", len(got), got, len(tc.wantIDs))
			}
			for i, want := range tc.wantIDs {
				if got[i].Workload != want {
					t.Errorf("[%d] workload = %q, want %q", i, got[i].Workload, want)
				}
				if got[i].DetectorID != "idle-workload-observed" {
					t.Errorf("[%d] DetectorID = %q", i, got[i].DetectorID)
				}
			}
		})
	}
}

func TestDetector_AnalyzeRejectsNegativeThresholds(t *testing.T) {
	d := &Detector{Threshold: IdleThreshold{MaxP95CPUMilli: -1}}
	_, err := d.Analyze(context.Background(), tenancy.Context{TenantID: "t"}, nil)
	if err == nil {
		t.Error("expected negative-threshold error")
	}
}
