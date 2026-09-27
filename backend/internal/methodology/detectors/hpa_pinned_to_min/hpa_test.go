package hpa_pinned_to_min

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
		count   int
	}{
		{
			name: "pinned-at-min with low CPU fires",
			samples: []Sample{{
				Workload: "api", MinReplicas: 4, MaxReplicas: 10, CurrentReplicas: 4,
				TargetCPUUtilPct: 70, AvgCPUUtilPct: 12, MonthlyCostUSDCents: 8000,
			}},
			count: 1,
		},
		{
			name: "above-min skips",
			samples: []Sample{{
				Workload: "scaling", MinReplicas: 4, MaxReplicas: 10, CurrentReplicas: 7,
				TargetCPUUtilPct: 70, AvgCPUUtilPct: 65,
			}},
			count: 0,
		},
		{
			name: "min=1 skips (no headroom to drop)",
			samples: []Sample{{
				Workload: "tiny", MinReplicas: 1, MaxReplicas: 4, CurrentReplicas: 1,
				TargetCPUUtilPct: 70, AvgCPUUtilPct: 5,
			}},
			count: 0,
		},
		{
			name: "no CPU target skips (memory-only HPA, ambiguous)",
			samples: []Sample{{
				Workload: "memhpa", MinReplicas: 3, MaxReplicas: 9, CurrentReplicas: 3,
				TargetCPUUtilPct: 0, AvgCPUUtilPct: 1,
			}},
			count: 0,
		},
		{
			name: "load above ratio skips (workload earns its floor)",
			samples: []Sample{{
				Workload: "spiky", MinReplicas: 3, MaxReplicas: 8, CurrentReplicas: 3,
				TargetCPUUtilPct: 70, AvgCPUUtilPct: 45,
			}},
			count: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := d.Analyze(ctx, tnt, tc.samples)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) != tc.count {
				t.Errorf("findings = %d (%+v), want %d", len(out), out, tc.count)
			}
		})
	}
}

func TestDetector_RejectsNegativeRatio(t *testing.T) {
	d := &Detector{Threshold: Threshold{MaxLoadRatio: -0.1}}
	_, err := d.Analyze(context.Background(), tenancy.Context{TenantID: "t"}, nil)
	if err == nil {
		t.Error("expected negative-ratio error")
	}
}
