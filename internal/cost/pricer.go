// Package cost projects monthly USD-cents for a parser.Workload from
// its declared requests and a region-aware Pricer. The engine is pure
// math: the CLI feeds a StaticPricer for sandbox ±40%, the backend
// feeds a CUR-backed Pricer for agent ±15%.
//
// Limits never enter the savings calculation; overprovisioning is a
// request finding, not a limit finding.
package cost

import (
	"fmt"

	"github.com/optiqor/optiqor/internal/parser"
)

// Pricer returns per-month USD-cents for a vCPU and a GiB of memory.
// Must be deterministic per region; callers cache results across many
// workloads in one analysis.
type Pricer interface {
	VCPUPerMonthUSDCents(region string) (int64, error)
	GiBMemoryPerMonthUSDCents(region string) (int64, error)
}

// Estimate carries USD-cents (platform-wide wire format) plus a Note
// the renderer surfaces so users see what went into the number.
type Estimate struct {
	Workload         string `json:"workload"`
	Region           string `json:"region"`
	Replicas         int    `json:"replicas"`
	CPUMillicores    int64  `json:"cpu_millicores"`
	MemoryBytes      int64  `json:"memory_bytes"`
	MonthlyUSDCents  int64  `json:"monthly_usd_cents"`
	CPUMonthlyCents  int64  `json:"cpu_monthly_cents"`
	MemMonthlyCents  int64  `json:"mem_monthly_cents"`
	Note             string `json:"note"`
	AccuracyBandPct  int    `json:"accuracy_band_pct"` // ±40 for sandbox, ±15 for agent
	UnpriceableField string `json:"unpriceable_field,omitempty"`
}

// Estimator is constructed once per analysis and reused across workloads.
type Estimator struct {
	Pricer          Pricer
	Region          string
	AccuracyBandPct int // ±40 from CLI/sandbox, ±15 from agent
}

const bytesPerGiB int64 = 1024 * 1024 * 1024

// 730-hour month is the canonical on-demand pricing basis across AWS,
// Azure, and GCP.
const hoursPerMonth int64 = 730

// Estimate never invents a number: a workload with no declared request
// returns a zero estimate plus an unpriceable note, not an error.
// Errors are infrastructure-class (Pricer failing).
func (e *Estimator) Estimate(w parser.Workload) (Estimate, error) {
	band := e.AccuracyBandPct
	if band <= 0 {
		band = 40 // never claim agent accuracy without an agent
	}
	out := Estimate{
		Workload:        w.Name,
		Region:          e.Region,
		Replicas:        clampReplicas(w.Replicas),
		AccuracyBandPct: band,
	}
	if !w.Requests.CPU.Set && !w.Requests.Memory.Set {
		out.UnpriceableField = "requests.cpu+memory"
		out.Note = "workload declares no resource requests; cannot estimate cost without an agent's measured baseline"
		return out, nil
	}

	if w.Requests.CPU.Set {
		out.CPUMillicores = w.Requests.CPU.Value
		vcpu, err := e.Pricer.VCPUPerMonthUSDCents(e.Region)
		if err != nil {
			return Estimate{}, fmt.Errorf("cost: pricing vCPU for %s: %w", e.Region, err)
		}
		out.CPUMonthlyCents = vcpu * w.Requests.CPU.Value * int64(out.Replicas) / 1000
	}
	if w.Requests.Memory.Set {
		out.MemoryBytes = w.Requests.Memory.Value
		gib, err := e.Pricer.GiBMemoryPerMonthUSDCents(e.Region)
		if err != nil {
			return Estimate{}, fmt.Errorf("cost: pricing memory for %s: %w", e.Region, err)
		}
		out.MemMonthlyCents = gib * w.Requests.Memory.Value * int64(out.Replicas) / bytesPerGiB
	}
	out.MonthlyUSDCents = out.CPUMonthlyCents + out.MemMonthlyCents

	switch {
	case out.CPUMonthlyCents > 0 && out.MemMonthlyCents > 0:
		out.Note = fmt.Sprintf("cpu+mem requests across %d replica(s) at %s on-demand pricing", out.Replicas, e.Region)
	case out.CPUMonthlyCents > 0:
		out.Note = fmt.Sprintf("cpu-only request across %d replica(s) at %s on-demand pricing", out.Replicas, e.Region)
	case out.MemMonthlyCents > 0:
		out.Note = fmt.Sprintf("memory-only request across %d replica(s) at %s on-demand pricing", out.Replicas, e.Region)
	}
	return out, nil
}

func Total(es []Estimate) int64 {
	var sum int64
	for _, e := range es {
		sum += e.MonthlyUSDCents
	}
	return sum
}

func clampReplicas(r int) int {
	if r <= 0 {
		return 1 // unset matches the Kubernetes default
	}
	return r
}

// HourlyRateCents converts $/vCPU·month or $/GiB·month into $/hour so
// dashboards can show the hourly anchor next to the monthly figure.
func HourlyRateCents(monthlyCents int64) int64 {
	return monthlyCents / hoursPerMonth
}
