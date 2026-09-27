// Package idle_workload_observed is the agent-mode counterpart to the
// CLI's sandbox-grade `idle-workload` detector. The CLI flags
// `replicas=0 && !HasHPA` from static Helm input; this detector flags
// `replicas > 0 && no traffic && no CPU for 7d` from observed cluster
// data. Replaces sandbox-grade detection with measured signals.
package idle_workload_observed

import (
	"context"
	"errors"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// Sample is one workload's observed state over the analysis window.
type Sample struct {
	Workload            string
	Namespace           string
	Replicas            int
	HasHPA              bool
	P95CPUMilli         int64 // 7-day p95
	NetworkBytes7d      int64
	RequestsCPUm        int64
	MonthlyCostUSDCents int64
}

// IdleThreshold is the contract: a workload with these signal levels
// or lower is considered idle. Defaults match the playbook spec.
type IdleThreshold struct {
	MaxP95CPUMilli int64
	MaxNetworkB7d  int64
}

// Default is conservative on purpose; lowers false-positive rate so
// the recommendation lands with high confidence.
func Default() IdleThreshold {
	return IdleThreshold{MaxP95CPUMilli: 10, MaxNetworkB7d: 1024 * 1024}
}

// Detector runs samples through the threshold and emits a Finding per
// idle workload. Pure func; no goroutines, no network.
type Detector struct {
	Threshold IdleThreshold
}

func NewDetector() *Detector { return &Detector{Threshold: Default()} }

// Analyze returns one Finding per idle workload in samples. Returns
// nil when no samples are idle so callers never have to nil-check.
func (d *Detector) Analyze(_ context.Context, _ tenancy.Context, samples []Sample) ([]rules.Finding, error) {
	if d.Threshold.MaxP95CPUMilli < 0 || d.Threshold.MaxNetworkB7d < 0 {
		return nil, errors.New("idle_workload_observed: negative threshold")
	}
	var out []rules.Finding
	for _, s := range samples {
		if s.Replicas <= 0 {
			continue // sandbox detector covers replicas=0
		}
		if s.HasHPA && s.P95CPUMilli > 0 {
			continue // HPA's already scaled it down to its min
		}
		if s.P95CPUMilli > d.Threshold.MaxP95CPUMilli {
			continue
		}
		if s.NetworkBytes7d > d.Threshold.MaxNetworkB7d {
			continue
		}
		out = append(out, rules.Finding{
			DetectorID:      "idle-workload-observed",
			Severity:        rules.SeverityHigh,
			Category:        rules.CategoryCost,
			Workload:        s.Workload,
			Title:           "Workload appears idle over the last 7 days",
			Detail:          "P95 CPU under " + cpuFmt(d.Threshold.MaxP95CPUMilli) + " and network < 1 MiB",
			MonthlyUSDCents: s.MonthlyCostUSDCents,
		})
	}
	return out, nil
}

func cpuFmt(milli int64) string {
	if milli >= 1000 && milli%1000 == 0 {
		return itoa(milli/1000) + " vCPU"
	}
	return itoa(milli) + "m"
}

func itoa(i int64) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
