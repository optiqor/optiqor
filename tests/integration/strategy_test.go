//go:build integration

package integration

import (
	"testing"

	"github.com/optiqor/optiqor/internal/cost/strategy"
	"github.com/optiqor/optiqor/internal/safety/environment"
)

// TestStrategy_EnvironmentTiers pins the per-environment aggressiveness
// contract Phase-4 commits to: prod stays the strictest, dev the
// loosest, staging in the middle, unknown maps to prod.
func TestStrategy_EnvironmentTiers(t *testing.T) {
	prod := strategy.For(environment.EnvProd)
	staging := strategy.For(environment.EnvStaging)
	dev := strategy.For(environment.EnvDev)
	unknown := strategy.For(environment.EnvUnknown)

	if prod != unknown {
		t.Errorf("unknown must alias prod (fail-safe); prod=%+v unknown=%+v", prod, unknown)
	}
	if prod.PercentileTarget != 99 || staging.PercentileTarget != 95 || dev.PercentileTarget != 95 {
		t.Errorf("percentile targets wrong: prod=%d staging=%d dev=%d", prod.PercentileTarget, staging.PercentileTarget, dev.PercentileTarget)
	}
	if !prod.ManualApproval || staging.ManualApproval || dev.ManualApproval {
		t.Errorf("manual approval should only fire in prod; got prod=%v staging=%v dev=%v", prod.ManualApproval, staging.ManualApproval, dev.ManualApproval)
	}
	if dev.AutoMergeEligible == false {
		t.Error("dev must be auto-merge eligible")
	}
}

func TestStrategy_MemoryCutBounds(t *testing.T) {
	for _, tc := range []struct {
		env  environment.Environment
		pct  int
		want bool
	}{
		{environment.EnvProd, 5, true},
		{environment.EnvProd, 11, false},
		{environment.EnvStaging, 20, true},
		{environment.EnvStaging, 26, false},
		{environment.EnvDev, 50, false}, // 0-cap rejects positive cuts
		{environment.EnvDev, 0, true},
	} {
		t.Run(string(tc.env), func(t *testing.T) {
			got := strategy.For(tc.env).MemoryCutAllowed(tc.pct)
			if got != tc.want {
				t.Errorf("MemoryCutAllowed(%d) in env=%s = %v, want %v", tc.pct, tc.env, got, tc.want)
			}
		})
	}
}
