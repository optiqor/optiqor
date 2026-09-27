// Package environment classifies a cluster + namespace pair so the
// cost engine can pick a per-environment aggressiveness profile.
// Fail-safe: when in doubt, treat as prod. See todo.md
// production-readiness Gap #7.
package environment

import "strings"

// Environment values are wire-stable and must mirror the
// clusters.environment CHECK constraint in migrations/0001_baseline.sql.
type Environment string

const (
	EnvProd    Environment = "prod"
	EnvStaging Environment = "staging"
	EnvDev     Environment = "dev"
	EnvUnknown Environment = "unknown"
)

// Aggressiveness tells the cost engine how far to push sizing changes.
// Conservative: P99 sizing, memory cuts capped at 10%, no replica
// reductions per PR. Medium: P95 sizing, memory cuts up to 25%.
// Aggressive: P95 sizing, full range.
type Aggressiveness string

const (
	AggressivenessConservative Aggressiveness = "conservative"
	AggressivenessMedium       Aggressiveness = "medium"
	AggressivenessAggressive   Aggressiveness = "aggressive"
)

type Profile struct {
	Environment       Environment
	Aggressiveness    Aggressiveness
	ConfidenceFloor   string // "high" | "medium" | "low"
	AutoMergeEligible bool
	ManualApproval    bool
}

// Classify resolves environment in order: customRules (exact ns
// match), cluster label, namespace label, cluster name pattern,
// namespace name pattern, else EnvUnknown (which ProfileFor maps to
// prod). Nil maps and empty strings are tolerated.
func Classify(clusterName string, clusterLabels, nsLabels map[string]string, ns string, customRules map[string]Environment) Environment {
	if customRules != nil {
		if env, ok := customRules[ns]; ok && env != "" {
			return env
		}
	}
	if env := envFromLabel(clusterLabels); env != EnvUnknown {
		return env
	}
	if env := envFromLabel(nsLabels); env != EnvUnknown {
		return env
	}
	if env := envFromName(clusterName); env != EnvUnknown {
		return env
	}
	if env := envFromName(ns); env != EnvUnknown {
		return env
	}
	return EnvUnknown
}

// ProfileFor returns the safety profile for a classification.
// EnvUnknown maps to the prod profile by design.
func ProfileFor(e Environment) Profile {
	switch e {
	case EnvDev:
		return Profile{
			Environment:       EnvDev,
			Aggressiveness:    AggressivenessAggressive,
			ConfidenceFloor:   "low",
			AutoMergeEligible: true,
			ManualApproval:    false,
		}
	case EnvStaging:
		return Profile{
			Environment:       EnvStaging,
			Aggressiveness:    AggressivenessMedium,
			ConfidenceFloor:   "medium",
			AutoMergeEligible: false,
			ManualApproval:    false,
		}
	default: // EnvProd + EnvUnknown share the fail-safe profile
		return Profile{
			Environment:       EnvProd,
			Aggressiveness:    AggressivenessConservative,
			ConfidenceFloor:   "high",
			AutoMergeEligible: false,
			ManualApproval:    true,
		}
	}
}

func envFromLabel(labels map[string]string) Environment {
	if labels == nil {
		return EnvUnknown
	}
	for _, k := range []string{"environment", "env", "tier"} {
		if v, ok := labels[k]; ok {
			return canonical(v)
		}
	}
	return EnvUnknown
}

func envFromName(name string) Environment {
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, "-prod"),
		strings.HasPrefix(n, "production-"),
		strings.HasPrefix(n, "prod-"),
		n == "prod" || n == "production":
		return EnvProd
	case strings.HasSuffix(n, "-staging"),
		strings.HasSuffix(n, "-stg"),
		strings.HasPrefix(n, "staging-"),
		n == "staging" || n == "stg":
		return EnvStaging
	case strings.HasSuffix(n, "-dev"),
		strings.HasPrefix(n, "dev-"),
		strings.HasPrefix(n, "development-"),
		strings.HasSuffix(n, "-development"),
		n == "dev" || n == "development":
		return EnvDev
	}
	return EnvUnknown
}

func canonical(v string) Environment {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "prod", "production":
		return EnvProd
	case "stg", "stage", "staging":
		return EnvStaging
	case "dev", "development":
		return EnvDev
	}
	return EnvUnknown
}
