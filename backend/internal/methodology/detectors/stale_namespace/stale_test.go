package stale_namespace

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
		name  string
		acts  []Activity
		count int
	}{
		{
			name:  "stale namespace fires",
			acts:  []Activity{{Namespace: "abandoned", WorkloadCount: 3, MonthlyCostUSDCents: 25000}},
			count: 1,
		},
		{
			name:  "active CPU skips",
			acts:  []Activity{{Namespace: "live", WorkloadCount: 2, CPUMilliSecondsObserved: 1, MonthlyCostUSDCents: 1000}},
			count: 0,
		},
		{
			name:  "active network skips",
			acts:  []Activity{{Namespace: "talkative", WorkloadCount: 1, NetworkBytes: 100, MonthlyCostUSDCents: 1000}},
			count: 0,
		},
		{
			name:  "empty namespace skips (not stale, just empty)",
			acts:  []Activity{{Namespace: "empty", WorkloadCount: 0}},
			count: 0,
		},
		{
			name:  "pod restart in window skips",
			acts:  []Activity{{Namespace: "flapping", WorkloadCount: 1, PodRestarts: 4, MonthlyCostUSDCents: 1000}},
			count: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := d.Analyze(ctx, tnt, tc.acts)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) != tc.count {
				t.Errorf("findings = %d (%+v), want %d", len(out), out, tc.count)
			}
		})
	}
}

func TestDetector_RejectsNegativeStaleDays(t *testing.T) {
	d := &Detector{StaleDays: -1}
	_, err := d.Analyze(context.Background(), tenancy.Context{TenantID: "t"}, nil)
	if err == nil {
		t.Error("expected negative-StaleDays error")
	}
}
