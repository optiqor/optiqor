// Package strategy holds the per-environment sizing-aggressiveness
// bundle the cost engine consults when generating recommendations.
// Reads environment.Profile and returns concrete numeric bounds the
// detector library and the validator pipeline gate against.
//
// Bounds match todo.md Phase-4 "Per-environment aggressiveness":
//   - prod:    P99 sizing, ≤10% memory cuts, no replica reductions per PR, High-conf only, manual approval
//   - staging: P95 sizing, ≤25% memory cuts, Medium+ confidence
//   - dev:     P95 sizing, no memory-cut cap, Low+ confidence, auto-merge eligible
package strategy

import (
	"errors"

	"github.com/optiqor/optiqor/internal/safety/environment"
)

// Sizing bundles the numeric thresholds the cost engine + validator
// gate against. Renamed from the looser "aggressiveness" string so
// the cost engine reads the values directly instead of branching on
// label strings.
type Sizing struct {
	// PercentileTarget is the headroom percentile the detector aims
	// for (95 or 99). Higher = more headroom, fewer recommendations.
	PercentileTarget int

	// MaxMemoryReductionPct caps how aggressively a memory request
	// can shrink in a single Apply Fix. 0 means "no cuts allowed"
	// (matches MemoryCutAllowed semantics); 100 = full range (dev).
	MaxMemoryReductionPct int

	// MaxReplicaReductionPerPR caps how many replicas a single PR can
	// remove. 0 means "no replica reductions allowed at all" (prod).
	MaxReplicaReductionPerPR int

	// MinConfidence is the floor a recommendation's confidence must
	// clear to be dispatched. Mirrors the rules package band.
	MinConfidence string // "high" | "medium" | "low"

	// AutoMergeEligible enables the dashboard's auto-merge opt-in for
	// this environment. Prod always false.
	AutoMergeEligible bool

	// ManualApproval blocks dispatch until a human approves. Prod
	// always true so a customer's CFO sees the change.
	ManualApproval bool
}

// For returns the Sizing bundle for an environment. Unknown
// environments map to prod by design (fail-safe).
func For(env environment.Environment) Sizing {
	switch env {
	case environment.EnvDev:
		return Sizing{
			PercentileTarget:         95,
			MaxMemoryReductionPct:    100, // full range per Phase-4 spec
			MaxReplicaReductionPerPR: 100,
			MinConfidence:            "low",
			AutoMergeEligible:        true,
		}
	case environment.EnvStaging:
		return Sizing{
			PercentileTarget:         95,
			MaxMemoryReductionPct:    25,
			MaxReplicaReductionPerPR: 2,
			MinConfidence:            "medium",
		}
	default: // EnvProd + EnvUnknown share the fail-safe profile
		return Sizing{
			PercentileTarget:         99,
			MaxMemoryReductionPct:    10,
			MaxReplicaReductionPerPR: 0,
			MinConfidence:            "high",
			ManualApproval:           true,
		}
	}
}

// ConfidenceMeets returns true when the candidate's confidence band
// meets or exceeds the strategy's floor. "high" > "medium" > "low".
func (s Sizing) ConfidenceMeets(candidateConfidence string) bool {
	return confidenceRank(candidateConfidence) >= confidenceRank(s.MinConfidence)
}

// MemoryCutAllowed returns true when the proposed reduction percent
// is at or below the strategy's cap. 0 cap means "no cuts allowed";
// positive cap means "up to that percent". 100 means unbounded.
func (s Sizing) MemoryCutAllowed(reductionPct int) bool {
	if s.MaxMemoryReductionPct == 0 {
		return reductionPct == 0
	}
	return reductionPct <= s.MaxMemoryReductionPct
}

// ReplicaCutAllowed returns true when the proposed replica reduction
// fits the per-PR cap. 0 means "no replica reductions at all".
func (s Sizing) ReplicaCutAllowed(reductionCount int) bool {
	if reductionCount <= 0 {
		return true
	}
	return reductionCount <= s.MaxReplicaReductionPerPR
}

// ErrUnknownEnv is reserved for callers that explicitly want to fail
// when the environment classifier returns EnvUnknown instead of
// silently picking the prod fail-safe profile. For() never returns
// this error; it's used at the call site.
var ErrUnknownEnv = errors.New("strategy: environment classifier returned unknown")

func confidenceRank(c string) int {
	switch c {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}
