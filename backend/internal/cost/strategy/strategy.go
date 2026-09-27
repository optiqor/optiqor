// Package strategy returns the per-environment Sizing bundle the cost
// engine + validator pipeline gate against. The numbers are pinned by
// todo.md Phase-4 "Per-environment aggressiveness"; unknown env maps
// to prod fail-safe.
package strategy

import (
	"errors"

	"github.com/optiqor/optiqor/internal/agent/provisioner"
	"github.com/optiqor/optiqor/internal/safety/environment"
)

type Sizing struct {
	PercentileTarget         int    // 95 or 99
	MaxMemoryReductionPct    int    // 0 = no cuts allowed (prod-floor when MaxReplicaReductionPerPR is also 0)
	MaxReplicaReductionPerPR int    // 0 = no replica reductions
	MinConfidence            string // "high" | "medium" | "low"
	MaxAggressiveness        int    // 0..100; static node groups cap at 50, autoscaler/karpenter at 100
	AutoMergeEligible        bool
	ManualApproval           bool
}

// For returns the Sizing bundle for env. EnvUnknown aliases prod. The
// single-arg shape preserves call sites that don't yet know the
// provisioner class; it defaults to ClassKarpenter (full aggressiveness)
// because every shipped customer today runs Karpenter or autoscaler.
// New code should prefer ForClass.
func For(env environment.Environment) Sizing {
	return ForClass(env, provisioner.ClassKarpenter)
}

// ForClass blends the per-env aggressiveness with a node-provisioner
// cap. Static node groups (T3) cap aggressiveness at 50 and raise the
// MinConfidence to medium, matching the ROADMAP "T3 = manual-step
// caveat, confidence Medium" line. Karpenter (T1) and Autoscaler (T2)
// stay at 100.
func ForClass(env environment.Environment, class provisioner.Class) Sizing {
	s := baseSizing(env)
	s.MaxAggressiveness = aggressivenessCap(class)
	if class == provisioner.ClassStatic {
		if s.MinConfidence == "low" {
			s.MinConfidence = "medium"
		}
		// Replica reductions need manual node-group changes when there's
		// no autoscaler; cap at zero so PRs don't propose them.
		s.MaxReplicaReductionPerPR = 0
	}
	return s
}

func baseSizing(env environment.Environment) Sizing {
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

func aggressivenessCap(class provisioner.Class) int {
	switch class {
	case provisioner.ClassStatic:
		return 50
	case provisioner.ClassKarpenter, provisioner.ClassAutoscaler:
		return 100
	default:
		return 100 // unknown classifier falls through to full so the validator's other checks still gate
	}
}

func (s Sizing) ConfidenceMeets(candidateConfidence string) bool {
	return confidenceRank(candidateConfidence) >= confidenceRank(s.MinConfidence)
}

// MemoryCutAllowed treats EffectiveMemoryCap = 0 as "no cuts at all".
// The cap scales with MaxAggressiveness so a static-node cluster
// (Aggressiveness 50) gets half the env's base reduction allowance.
func (s Sizing) MemoryCutAllowed(reductionPct int) bool {
	maxPct := s.EffectiveMemoryCap()
	if maxPct == 0 {
		return reductionPct == 0
	}
	return reductionPct <= maxPct
}

func (s Sizing) ReplicaCutAllowed(reductionCount int) bool {
	if reductionCount <= 0 {
		return true
	}
	return reductionCount <= s.EffectiveReplicaCap()
}

// EffectiveMemoryCap is MaxMemoryReductionPct scaled by MaxAggressiveness.
// MaxAggressiveness 100 keeps the base cap; 50 halves it; 0 zeroes it.
// Exported so the validator and PR-writer can show the live ceiling.
func (s Sizing) EffectiveMemoryCap() int {
	return scaleByAggressiveness(s.MaxMemoryReductionPct, s.MaxAggressiveness)
}

// EffectiveReplicaCap mirrors EffectiveMemoryCap for replicas.
func (s Sizing) EffectiveReplicaCap() int {
	return scaleByAggressiveness(s.MaxReplicaReductionPerPR, s.MaxAggressiveness)
}

func scaleByAggressiveness(base, agg int) int {
	if agg <= 0 || base <= 0 {
		return 0
	}
	if agg >= 100 {
		return base
	}
	return base * agg / 100
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
