package vpa_divergence

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
			name: "Off-mode CPU 50% lower fires",
			samples: []Sample{{
				Workload: "api", Mode: "Off",
				CurrentCPUm: 1000, RecommendCPUm: 500,
				CurrentMemB: 1 << 30, RecommendMemB: 1 << 30,
				MonthlyCostUSDCents: 8000,
			}},
			count: 1,
		},
		{
			name: "Initial-mode mem 30% lower fires",
			samples: []Sample{{
				Workload: "ingest", Mode: "Initial",
				CurrentCPUm: 200, RecommendCPUm: 200,
				CurrentMemB: 1 << 30, RecommendMemB: (1 << 30) * 7 / 10,
				MonthlyCostUSDCents: 4000,
			}},
			count: 1,
		},
		{
			name: "Auto-mode skipped (VPA already actuating)",
			samples: []Sample{{
				Workload: "auto", Mode: "Auto",
				CurrentCPUm: 1000, RecommendCPUm: 200,
			}},
			count: 0,
		},
		{
			name: "divergence under threshold skips",
			samples: []Sample{{
				Workload: "tight", Mode: "Off",
				CurrentCPUm: 1000, RecommendCPUm: 900,
				CurrentMemB: 1 << 30, RecommendMemB: 1 << 30,
			}},
			count: 0,
		},
		{
			name: "recommend higher than current skips (raise-not-lower)",
			samples: []Sample{{
				Workload: "tight", Mode: "Off",
				CurrentCPUm: 500, RecommendCPUm: 800,
			}},
			count: 0,
		},
		{
			name: "missing VPA reco (0) skips",
			samples: []Sample{{
				Workload: "no-vpa", Mode: "Off",
				CurrentCPUm: 500, RecommendCPUm: 0,
				CurrentMemB: 1 << 30, RecommendMemB: 0,
			}},
			count: 0,
		},
		{
			name: "unknown mode skips",
			samples: []Sample{{
				Workload: "wat", Mode: "",
				CurrentCPUm: 1000, RecommendCPUm: 100,
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

func TestDetector_RejectsNegativeDivergence(t *testing.T) {
	d := &Detector{Threshold: Threshold{MinDivergence: -0.1}}
	_, err := d.Analyze(context.Background(), tenancy.Context{TenantID: "t"}, nil)
	if err == nil {
		t.Error("expected negative-divergence error")
	}
}
