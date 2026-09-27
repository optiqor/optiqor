package oomkilled_observed

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
			name:    "single OOMKill fires",
			samples: []Sample{{Workload: "api", OOMKillCount: 1, MonthlyCostUSDCents: 5000}},
			count:   1,
		},
		{
			name:    "no OOMKills skips",
			samples: []Sample{{Workload: "calm", OOMKillCount: 0}},
			count:   0,
		},
		{
			name: "mixed only-OOMs fire",
			samples: []Sample{
				{Workload: "noisy", OOMKillCount: 7, MonthlyCostUSDCents: 4000},
				{Workload: "stable", OOMKillCount: 0},
			},
			count: 1,
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

func TestDetector_HonorsThresholdOverride(t *testing.T) {
	d := &Detector{Threshold: Threshold{MinKills: 5}}
	out, err := d.Analyze(context.Background(), tenancy.Context{TenantID: "t"}, []Sample{
		{Workload: "flapping", OOMKillCount: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("expected MinKills=5 to suppress 3 kills, got %d findings", len(out))
	}
}

func TestDetector_RejectsNegativeThreshold(t *testing.T) {
	d := &Detector{Threshold: Threshold{MinKills: -1}}
	_, err := d.Analyze(context.Background(), tenancy.Context{TenantID: "t"}, nil)
	if err == nil {
		t.Error("expected negative-MinKills error")
	}
}
