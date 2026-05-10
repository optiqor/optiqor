package cost

import (
	"errors"
	"testing"

	"github.com/optiqor/optiqor-cli/pkg/parser"
)

func mkWorkload(cpuMilli, memBytes int64, replicas int) parser.Workload {
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

func TestEstimator_NoRequests_UnpriceableNotError(t *testing.T) {
	e := &Estimator{Pricer: NewStaticPricer(), Region: "us-east-1"}
	w := parser.Workload{Name: "weird"}
	est, err := e.Estimate(w)
	if err != nil {
		t.Fatalf("unpriceable workload should not error: %v", err)
	}
	if est.MonthlyUSDCents != 0 {
		t.Errorf("monthly = %d, want 0", est.MonthlyUSDCents)
	}
	if est.UnpriceableField == "" {
		t.Error("unpriceable_field should be populated")
	}
	if est.AccuracyBandPct != 40 {
		t.Errorf("band = %d, want 40 default", est.AccuracyBandPct)
	}
}

func TestEstimator_CPUOnly_LinearInMillicores(t *testing.T) {
	e := &Estimator{Pricer: NewStaticPricer(), Region: "us-east-1"}
	// 500m × 1 replica × 3504 c/vCPU·month = 500 × 3504 / 1000 = 1752
	est, err := e.Estimate(mkWorkload(500, 0, 1))
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if est.CPUMonthlyCents != 1752 {
		t.Errorf("cpu monthly = %d, want 1752", est.CPUMonthlyCents)
	}
	if est.MonthlyUSDCents != 1752 {
		t.Errorf("monthly = %d, want 1752", est.MonthlyUSDCents)
	}
}

func TestEstimator_MemoryOnly_LinearInBytes(t *testing.T) {
	e := &Estimator{Pricer: NewStaticPricer(), Region: "us-east-1"}
	// 1 GiB × 1 replica × 876 c/GiB·month
	gib := int64(1024 * 1024 * 1024)
	est, err := e.Estimate(mkWorkload(0, gib, 1))
	if err != nil {
		t.Fatalf("Estimate: %v", err)
	}
	if est.MemMonthlyCents != 876 {
		t.Errorf("mem monthly = %d, want 876", est.MemMonthlyCents)
	}
}

func TestEstimator_RepliesScaleLinear(t *testing.T) {
	e := &Estimator{Pricer: NewStaticPricer(), Region: "us-east-1"}
	one, _ := e.Estimate(mkWorkload(1000, 0, 1))
	five, _ := e.Estimate(mkWorkload(1000, 0, 5))
	if five.MonthlyUSDCents != one.MonthlyUSDCents*5 {
		t.Errorf("5×1-replica should equal 5-replica: %d vs %d", five.MonthlyUSDCents, one.MonthlyUSDCents*5)
	}
}

func TestEstimator_UnsetReplicasTreatedAsOne(t *testing.T) {
	e := &Estimator{Pricer: NewStaticPricer(), Region: "us-east-1"}
	zero, _ := e.Estimate(mkWorkload(1000, 0, 0))
	one, _ := e.Estimate(mkWorkload(1000, 0, 1))
	if zero.MonthlyUSDCents != one.MonthlyUSDCents {
		t.Errorf("replicas=0 should clamp to 1: %d vs %d", zero.MonthlyUSDCents, one.MonthlyUSDCents)
	}
}

func TestEstimator_RegionMissing_Propagates(t *testing.T) {
	e := &Estimator{Pricer: NewStaticPricer(), Region: "mars-east-1"}
	_, err := e.Estimate(mkWorkload(500, 0, 1))
	if err == nil {
		t.Fatal("want error when region missing")
	}
}

func TestEstimator_AccuracyBandHonoured(t *testing.T) {
	e := &Estimator{Pricer: NewStaticPricer(), Region: "us-east-1", AccuracyBandPct: 15}
	est, _ := e.Estimate(mkWorkload(500, 0, 1))
	if est.AccuracyBandPct != 15 {
		t.Errorf("band = %d, want 15 (agent accuracy)", est.AccuracyBandPct)
	}
}

func TestTotal_SumsAcrossWorkloads(t *testing.T) {
	es := []Estimate{{MonthlyUSDCents: 100}, {MonthlyUSDCents: 250}, {MonthlyUSDCents: 0}}
	if got := Total(es); got != 350 {
		t.Errorf("Total = %d, want 350", got)
	}
}

func TestHourlyRateCents(t *testing.T) {
	// 3504 cents/month / 730 hours = 4.8 → integer 4
	if got := HourlyRateCents(3504); got != 4 {
		t.Errorf("HourlyRateCents = %d, want 4", got)
	}
}

// failingPricer always errors; used to assert the wrapper preserves the
// inner error chain rather than swallowing it.
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
	_, err := e.Estimate(mkWorkload(500, 0, 1))
	if !errors.Is(err, errSentinel) {
		t.Errorf("want error chain to include sentinel; got %v", err)
	}
}
