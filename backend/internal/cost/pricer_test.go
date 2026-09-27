package cost

import (
	"errors"
	"testing"

	"github.com/optiqor/optiqor-cli/pkg/parser"
)

func mkWorkload(t *testing.T, cpuMilli, memBytes int64, replicas int) parser.Workload {
	t.Helper()
	w := parser.Workload{Name: "api", Replicas: replicas}
	if cpuMilli > 0 {
		w.Requests.CPU = parser.Quantity{Value: cpuMilli, Set: true, Original: "set"}
	}
	if memBytes > 0 {
		w.Requests.Memory = parser.Quantity{Value: memBytes, Set: true, Original: "set"}
	}
	return w
}

func TestStaticPricer_AllSupportedRegionsPresent(t *testing.T) {
	p := NewStaticPricer()
	for _, r := range SupportedRegions {
		if _, err := p.VCPUPerMonthUSDCents(r); err != nil {
			t.Errorf("missing vCPU rate for %s: %v", r, err)
		}
		if _, err := p.GiBMemoryPerMonthUSDCents(r); err != nil {
			t.Errorf("missing memory rate for %s: %v", r, err)
		}
	}
}

func TestStaticPricer_UnknownRegionErrors(t *testing.T) {
	p := NewStaticPricer()
	if _, err := p.VCPUPerMonthUSDCents("mars-east-1"); err == nil {
		t.Error("want error for unknown region")
	}
}

func TestEstimator(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	for _, tc := range []struct {
		name            string
		region          string
		bandPct         int
		workload        parser.Workload
		wantErr         bool
		wantMonthly     int64
		wantCPUMonthly  int64
		wantMemMonthly  int64
		wantBand        int
		wantUnpriceable string
	}{
		{
			name:            "no requests",
			region:          "us-east-1",
			workload:        parser.Workload{Name: "weird"},
			wantBand:        40,
			wantUnpriceable: "requests.cpu+memory",
		},
		{
			name:           "cpu only 500m",
			region:         "us-east-1",
			workload:       mkWorkload(t, 500, 0, 1),
			wantCPUMonthly: 1752, // 500m × 3504 c/vCPU·month ÷ 1000
			wantMonthly:    1752,
			wantBand:       40,
		},
		{
			name:           "memory only 1 gib",
			region:         "us-east-1",
			workload:       mkWorkload(t, 0, gib, 1),
			wantMemMonthly: 876, // 1 GiB × 876 c/GiB·month
			wantMonthly:    876,
			wantBand:       40,
		},
		{
			name:           "replicas scale linear",
			region:         "us-east-1",
			workload:       mkWorkload(t, 1000, 0, 5),
			wantCPUMonthly: 17520, // 1 vCPU × 3504 × 5
			wantMonthly:    17520,
			wantBand:       40,
		},
		{
			name:           "replicas unset clamps to one",
			region:         "us-east-1",
			workload:       mkWorkload(t, 1000, 0, 0),
			wantCPUMonthly: 3504,
			wantMonthly:    3504,
			wantBand:       40,
		},
		{
			name:     "unknown region propagates",
			region:   "mars-east-1",
			workload: mkWorkload(t, 500, 0, 1),
			wantErr:  true,
		},
		{
			name:           "accuracy band override",
			region:         "us-east-1",
			bandPct:        15,
			workload:       mkWorkload(t, 500, 0, 1),
			wantCPUMonthly: 1752,
			wantMonthly:    1752,
			wantBand:       15,
		},
		{
			name:           "cpu plus memory",
			region:         "us-east-1",
			workload:       mkWorkload(t, 500, gib, 1),
			wantCPUMonthly: 1752,
			wantMemMonthly: 876,
			wantMonthly:    2628,
			wantBand:       40,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &Estimator{Pricer: NewStaticPricer(), Region: tc.region, AccuracyBandPct: tc.bandPct}
			est, err := e.Estimate(tc.workload)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Estimate: %v", err)
			}
			if est.MonthlyUSDCents != tc.wantMonthly {
				t.Errorf("monthly = %d, want %d", est.MonthlyUSDCents, tc.wantMonthly)
			}
			if est.CPUMonthlyCents != tc.wantCPUMonthly {
				t.Errorf("cpu monthly = %d, want %d", est.CPUMonthlyCents, tc.wantCPUMonthly)
			}
			if est.MemMonthlyCents != tc.wantMemMonthly {
				t.Errorf("mem monthly = %d, want %d", est.MemMonthlyCents, tc.wantMemMonthly)
			}
			if est.AccuracyBandPct != tc.wantBand {
				t.Errorf("band = %d, want %d", est.AccuracyBandPct, tc.wantBand)
			}
			if tc.wantUnpriceable != "" && est.UnpriceableField == "" {
				t.Errorf("unpriceable_field empty, want populated (%q)", tc.wantUnpriceable)
			}
			if tc.wantUnpriceable == "" && est.UnpriceableField != "" {
				t.Errorf("unpriceable_field = %q, want empty", est.UnpriceableField)
			}
		})
	}
}

// failingPricer asserts the Estimator preserves the inner error chain.
type failingPricer struct{}

func (failingPricer) VCPUPerMonthUSDCents(string) (int64, error) {
	return 0, errSentinel
}
func (failingPricer) GiBMemoryPerMonthUSDCents(string) (int64, error) {
	return 0, errSentinel
}

var errSentinel = errors.New("test sentinel")

func TestEstimator_PricerError_Wrapped(t *testing.T) {
	e := &Estimator{Pricer: failingPricer{}, Region: "us-east-1"}
	_, err := e.Estimate(mkWorkload(t, 500, 0, 1))
	if !errors.Is(err, errSentinel) {
		t.Errorf("want error chain to include sentinel; got %v", err)
	}
}

func TestTotal_SumsAcrossWorkloads(t *testing.T) {
	es := []Estimate{{MonthlyUSDCents: 100}, {MonthlyUSDCents: 250}, {MonthlyUSDCents: 0}}
	if got := Total(es); got != 350 {
		t.Errorf("Total = %d, want 350", got)
	}
}

func TestHourlyRateCents_TruncatesToInt(t *testing.T) {
	// 3504 ÷ 730 = 4.8 → 4 under integer division
	if got := HourlyRateCents(3504); got != 4 {
		t.Errorf("HourlyRateCents = %d, want 4", got)
	}
}
