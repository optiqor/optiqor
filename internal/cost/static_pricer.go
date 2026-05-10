package cost

import "fmt"

// StaticPricer is a region → ($/vCPU·month, $/GiB·month) table. Phase 1
// ships hard-coded baselines reflecting the average on-demand rate of
// the m6i instance family — close enough for the sandbox ±40% band.
// Agent customers run with a [LivePricer] backed by Cost Explorer.
//
// Numbers are *cents*. Update conservatively: a wrong value here
// changes every customer's reported savings.
type StaticPricer struct {
	rates map[string]rate
}

type rate struct {
	vcpuMonthlyCents int64
	gibMonthlyCents  int64
}

// NewStaticPricer returns a Pricer pre-loaded with the AWS regions our
// Phase-1 customers actually use. Add a region by extending the map;
// the unit test enforces that every key in [SupportedRegions] is
// present.
func NewStaticPricer() *StaticPricer {
	return &StaticPricer{rates: defaultRates()}
}

// SupportedRegions is the closed set returned by /v1/regions and
// asserted on by NewStaticPricer's invariant test. Kept stable so
// pricing surfaces (renderer, JSON) never get a region they cannot
// link to.
var SupportedRegions = []string{
	"us-east-1", "us-east-2", "us-west-2",
	"eu-central-1", "eu-west-1",
	"ap-south-1", "ap-southeast-1",
}

func defaultRates() map[string]rate {
	// Baseline: m6i.large on-demand, normalised to vCPU and GiB.
	// us-east-1 anchor: $0.096/hr for 2 vCPU + 8 GiB.
	//
	//   per-vCPU·hour cents  = 9.6 / 2     = 4.8
	//   per-GiB·hour cents   = 9.6 / 8     = 1.2
	//   monthly (×730)       = 3504 c/vCPU, 876 c/GiB
	//
	// Other regions scale by the published cross-region multipliers.
	return map[string]rate{
		"us-east-1":      {vcpuMonthlyCents: 3504, gibMonthlyCents: 876},
		"us-east-2":      {vcpuMonthlyCents: 3504, gibMonthlyCents: 876},
		"us-west-2":      {vcpuMonthlyCents: 3504, gibMonthlyCents: 876},
		"eu-central-1":   {vcpuMonthlyCents: 4380, gibMonthlyCents: 1095},
		"eu-west-1":      {vcpuMonthlyCents: 4205, gibMonthlyCents: 1051},
		"ap-south-1":     {vcpuMonthlyCents: 3329, gibMonthlyCents: 832},
		"ap-southeast-1": {vcpuMonthlyCents: 4117, gibMonthlyCents: 1029},
	}
}

// VCPUPerMonthUSDCents returns the monthly per-vCPU price for a region.
func (s *StaticPricer) VCPUPerMonthUSDCents(region string) (int64, error) {
	r, ok := s.rates[region]
	if !ok {
		return 0, fmt.Errorf("cost: unknown region %q", region)
	}
	return r.vcpuMonthlyCents, nil
}

// GiBMemoryPerMonthUSDCents returns the monthly per-GiB memory price.
func (s *StaticPricer) GiBMemoryPerMonthUSDCents(region string) (int64, error) {
	r, ok := s.rates[region]
	if !ok {
		return 0, fmt.Errorf("cost: unknown region %q", region)
	}
	return r.gibMonthlyCents, nil
}

// Regions returns the supported region keys in deterministic order.
func (s *StaticPricer) Regions() []string {
	out := make([]string, 0, len(s.rates))
	for k := range s.rates {
		out = append(out, k)
	}
	// stable sort without a heavyweight import
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
