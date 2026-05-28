// Package strategy returns the per-environment Sizing bundle the cost
// engine + validator pipeline gate against. The numbers are pinned by
// todo.md Phase-4 "Per-environment aggressiveness"; unknown env maps
// to prod fail-safe.
package strategy

import (
	"errors"

	"github.com/optiqor/optiqor/internal/safety/environment"
)

type Sizing struct {
	PercentileTarget         int    // 95 or 99
	MaxMemoryReductionPct    int    // 0 = no cuts allowed (prod-floor when MaxReplicaReductionPerPR is also 0)
	MaxReplicaReductionPerPR int    // 0 = no replica reductions
	MinConfidence            string // "high" | "medium" | "low"
	AutoMergeEligible        bool
	ManualApproval           bool
}

// For returns the Sizing bundle for env. EnvUnknown aliases prod.
func For(env environment.Environment) Sizing {
	switch env {
	case environment.EnvDev:
		return Sizing{
			PercentileTarget:         95,
			MaxMemoryReductionPct:    0,
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

func (s Sizing) ConfidenceMeets(candidateConfidence string) bool {
	return confidenceRank(candidateConfidence) >= confidenceRank(s.MinConfidence)
}

// MemoryCutAllowed treats MaxMemoryReductionPct = 0 as "no cuts at all".
func (s Sizing) MemoryCutAllowed(reductionPct int) bool {
	if s.MaxMemoryReductionPct == 0 {
		return reductionPct == 0
	}
	return reductionPct <= s.MaxMemoryReductionPct
}

func (s Sizing) ReplicaCutAllowed(reductionCount int) bool {
	if reductionCount <= 0 {
		return true
	}
	return reductionCount <= s.MaxReplicaReductionPerPR
}

// ErrUnknownEnv is for callers that want to fail explicitly on
// EnvUnknown instead of falling through to the prod fail-safe.
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
