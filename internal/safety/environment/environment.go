// Package environment classifies a cluster + namespace pair as
// prod / staging / dev / unknown so the cost engine can pick a
// per-environment aggressiveness profile.
//
// The fail-safe rule is: when in doubt, treat as prod. A first prod
// incident from "Sevro cut our payments memory and we OOMed during
// peak traffic" is what kills a Year-1 trust contract — see todo.md
// production-readiness Gap #7.
package environment

import "strings"

// Environment is the classified bucket. Wire-stable; mirrors the
// clusters.environment CHECK constraint in migrations/0001_baseline.sql.
type Environment string

const (
	EnvProd    Environment = "prod"
	EnvStaging Environment = "staging"
	EnvDev     Environment = "dev"
	EnvUnknown Environment = "unknown"
)

// Aggressiveness tells the cost engine how far to push sizing changes.
type Aggressiveness string

const (
	// AggressivenessConservative — P99 sizing, no memory cuts >10%, no
	// replica reductions in a single PR.
	AggressivenessConservative Aggressiveness = "conservative"
	// AggressivenessMedium — P95 sizing, memory cuts up to 25%.
	AggressivenessMedium Aggressiveness = "medium"
	// AggressivenessAggressive — P95 sizing, full range.
	AggressivenessAggressive Aggressiveness = "aggressive"
)

// Profile bundles aggressiveness with confidence-floor and auto-merge
// rules so the cost engine and PR writer read one shape.
type Profile struct {
	Environment       Environment
	Aggressiveness    Aggressiveness
	ConfidenceFloor   string // "high" | "medium" | "low"
	AutoMergeEligible bool
	ManualApproval    bool
}

// Classify returns the environment for a cluster + namespace given the
// labels each carries plus customer-provided rules. Order of precedence:
//
//  1. CustomRules (highest) — exact namespace match wins
//  2. Cluster `environment` label
//  3. Namespace `environment` label
//  4. Cluster name pattern (`*-prod`, `production-*`, `*-stg`, `*-dev`)
//  5. Namespace name pattern
//  6. EnvUnknown — and EnvUnknown gets the prod profile (fail-safe)
//
// Empty strings are tolerated everywhere; nil maps are treated as empty.
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

// ProfileFor returns the safety profile to apply for a classification.
// EnvUnknown maps to the prod profile by design — the fail-safe.
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
	default: // EnvProd or EnvUnknown — fail-safe prod
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
