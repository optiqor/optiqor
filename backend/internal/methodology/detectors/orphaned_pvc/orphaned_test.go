package orphaned_pvc

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
		pvcs  []PVCRef
		count int
	}{
		{
			name:  "old + unreferenced fires",
			pvcs:  []PVCRef{{Namespace: "default", Name: "data-pg-0", AgeDays: 14, CapacityBytes: 100 << 30}},
			count: 1,
		},
		{
			name:  "young unreferenced skipped",
			pvcs:  []PVCRef{{Namespace: "default", Name: "fresh", AgeDays: 3}},
			count: 0,
		},
		{
			name:  "referenced PVC skipped regardless of age",
			pvcs:  []PVCRef{{Namespace: "default", Name: "active", AgeDays: 90, Referenced: true}},
			count: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := d.Analyze(ctx, tnt, tc.pvcs)
			if err != nil {
				t.Fatal(err)
			}
			if len(out) != tc.count {
				t.Errorf("findings = %d (%+v), want %d", len(out), out, tc.count)
			}
		})
	}
}

func TestDetector_RejectsNegativeMinAge(t *testing.T) {
	d := &Detector{MinAge: -1}
	_, err := d.Analyze(context.Background(), tenancy.Context{TenantID: "t"}, nil)
	if err == nil {
		t.Error("expected negative-MinAge error")
	}
}
