package cost

import "fmt"

// StaticPricer hard-codes the m6i-family on-demand average per region;
// sandbox ±40% band. Backend swaps to LivePricer in Phase 5; the CLI
// keeps its own static table forever for OSS reproducibility — don't
// try to keep the two in sync.
type StaticPricer struct {
	rates map[string]rate
}

type rate struct {
	vcpuMonthlyCents int64
	gibMonthlyCents  int64
}

// NewStaticPricer is invariant-tested: every SupportedRegions entry
// must have a rate.
func NewStaticPricer() *StaticPricer {
	return &StaticPricer{rates: defaultRates()}
}

// SupportedRegions is the closed set returned by /v1/regions. Kept
// stable so the renderer never gets a region it can't link to.
var SupportedRegions = []string{
	"us-east-1", "us-east-2", "us-west-2",
	"eu-central-1", "eu-west-1",
	"ap-south-1", "ap-southeast-1",
}

func defaultRates() map[string]rate {
	// us-east-1 anchor: m6i.large on-demand $0.096/hr for 2 vCPU + 8 GiB.
	//   per-vCPU·hour cents  = 9.6 / 2 = 4.8 → 3504 / month
	//   per-GiB·hour cents   = 9.6 / 8 = 1.2 → 876 / month
	// Other regions scale by published cross-region multipliers.
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

func (s *StaticPricer) VCPUPerMonthUSDCents(region string) (int64, error) {
	r, ok := s.rates[region]
	if !ok {
		return 0, fmt.Errorf("cost: unknown region %q", region)
	}
	return r.vcpuMonthlyCents, nil
}

func (s *StaticPricer) GiBMemoryPerMonthUSDCents(region string) (int64, error) {
	r, ok := s.rates[region]
	if !ok {
		return 0, fmt.Errorf("cost: unknown region %q", region)
	}
	return r.gibMonthlyCents, nil
}

// Regions returns supported region keys sorted for deterministic output.
func (s *StaticPricer) Regions() []string {
	out := make([]string, 0, len(s.rates))
	for k := range s.rates {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
