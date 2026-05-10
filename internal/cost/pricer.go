// Package cost computes the dollar cost of a [parser.Workload] from
// its declared resource requests and a region-aware [Pricer].
//
// The CLI ships a sandbox-grade ±40% estimate because static files are
// all it sees; the backend reaches for the same engine but supplies a
// [Pricer] backed by live data (CUR, Pricing API) to deliver the
// ±10–15% accuracy the agent customers pay for. The engine itself is
// pure math — no I/O — so it stays the same regardless of who is
// asking for the number.
//
// Pricing semantics:
//
//   - CPU is amortised as $/vCPU·month, computed from the on-demand
//     hourly rate of the customer-selected instance class.
//   - Memory is amortised as $/GiB·month, on the same hour basis.
//   - The estimator multiplies the workload's request by replicas
//     (declared or HPA min if known). Limits are an upper bound but
//     never enter the savings calculation — overprovisioning is a CPU
//     request finding, not a limit finding.
//   - Cents are the wire format; the JSON layer divides by 100 for
//     display so the renderer never sees floats.
package cost

import (
	"fmt"

	"github.com/optiqor/backend/internal/parser"
)

// Pricer returns per-month USD-cents for a vCPU and a GiB of memory in
// the given region. Implementations must be deterministic for a given
// region; callers will cache results across many workloads in a single
// analysis.
type Pricer interface {
	VCPUPerMonthUSDCents(region string) (int64, error)
	GiBMemoryPerMonthUSDCents(region string) (int64, error)
}

// Estimate is the result of running [Estimator] on a single workload.
// Currency is USD-cents to match the rest of the platform; Note is
// surface-able prose describing what went into the number so users
// understand the math.
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

// Estimator wraps a [Pricer] and configures how the engine annotates
// results. Construct once per analysis; reuse across workloads.
type Estimator struct {
	Pricer          Pricer
	Region          string
	AccuracyBandPct int // ±40 from CLI/sandbox, ±15 from agent
}

// Bytes per GiB. Used for memory cost normalisation.
const bytesPerGiB int64 = 1024 * 1024 * 1024

// minutesPerMonth is the standard 730-hour month used by every major
// cloud's on-demand pricing.
const hoursPerMonth int64 = 730

// Estimate returns the per-month cost for w. A workload with no
// declared request gets a zero estimate and an "unpriceable" note —
// the engine never invents a number from nothing.
//
// Errors here are infrastructure-class (the Pricer failing); a
// workload missing requests is a normal outcome, not an error.
func (e *Estimator) Estimate(w parser.Workload) (Estimate, error) {
	band := e.AccuracyBandPct
	if band <= 0 {
		band = 40 // CLI/sandbox default — never claim agent accuracy without an agent.
	}
	out := Estimate{
		Workload:        w.Name,
		Region:          e.Region,
		Replicas:        clampReplicas(w.Replicas),
		AccuracyBandPct: band,
	}
	if !w.Requests.CPU.Set && !w.Requests.Memory.Set {
		out.UnpriceableField = "requests.cpu+memory"
		out.Note = "workload declares no resource requests — cannot estimate cost without an agent's measured baseline"
		return out, nil
	}

	if w.Requests.CPU.Set {
		out.CPUMillicores = w.Requests.CPU.Value
		vcpu, err := e.Pricer.VCPUPerMonthUSDCents(e.Region)
		if err != nil {
			return Estimate{}, fmt.Errorf("cost: pricing vCPU for %s: %w", e.Region, err)
		}
		// vcpu cents * (millicores/1000) * replicas
		out.CPUMonthlyCents = vcpu * w.Requests.CPU.Value * int64(out.Replicas) / 1000
	}
	if w.Requests.Memory.Set {
		out.MemoryBytes = w.Requests.Memory.Value
		gib, err := e.Pricer.GiBMemoryPerMonthUSDCents(e.Region)
		if err != nil {
			return Estimate{}, fmt.Errorf("cost: pricing memory for %s: %w", e.Region, err)
		}
		// gib cents * (bytes / bytesPerGiB) * replicas
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

// Total sums monthly cents across a slice of estimates.
func Total(es []Estimate) int64 {
	var sum int64
	for _, e := range es {
		sum += e.MonthlyUSDCents
	}
	return sum
}

func clampReplicas(r int) int {
	if r <= 0 {
		return 1 // unset → Kubernetes default
	}
	return r
}

// HourlyRateCents converts the canonical $/vCPU·month or $/GiB·month
// rate into an $/hour figure. Exposed so dashboards can show the
// hourly anchor next to the monthly number.
func HourlyRateCents(monthlyCents int64) int64 {
	return monthlyCents / hoursPerMonth
}
