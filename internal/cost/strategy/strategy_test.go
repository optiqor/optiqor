package strategy

import (
	"testing"

	"github.com/optiqor/optiqor/internal/safety/environment"
)

func TestFor(t *testing.T) {
	for _, tc := range []struct {
		env  environment.Environment
		want Sizing
	}{
		{
			env: environment.EnvProd,
			want: Sizing{
				PercentileTarget: 99, MaxMemoryReductionPct: 10, MaxReplicaReductionPerPR: 0,
				MinConfidence: "high", ManualApproval: true,
			},
		},
		{
			env: environment.EnvStaging,
			want: Sizing{
				PercentileTarget: 95, MaxMemoryReductionPct: 25, MaxReplicaReductionPerPR: 2,
				MinConfidence: "medium",
			},
		},
		{
			env: environment.EnvDev,
			want: Sizing{
				PercentileTarget: 95, MaxMemoryReductionPct: 100, MaxReplicaReductionPerPR: 100,
				MinConfidence: "low", AutoMergeEligible: true,
			},
		},
		{
			env: environment.EnvUnknown,
			want: Sizing{
				PercentileTarget: 99, MaxMemoryReductionPct: 10, MaxReplicaReductionPerPR: 0,
				MinConfidence: "high", ManualApproval: true,
			},
		},
	} {
		t.Run(string(tc.env), func(t *testing.T) {
			got := For(tc.env)
			if got != tc.want {
				t.Errorf("For(%q) = %+v, want %+v", tc.env, got, tc.want)
			}
		})
	}
}

func TestSizing_ConfidenceMeets(t *testing.T) {
	prod := For(environment.EnvProd)
	dev := For(environment.EnvDev)

	for _, tc := range []struct {
		name      string
		strategy  Sizing
		candidate string
		want      bool
	}{
		{name: "prod accepts high", strategy: prod, candidate: "high", want: true},
		{name: "prod rejects medium", strategy: prod, candidate: "medium", want: false},
		{name: "dev accepts low", strategy: dev, candidate: "low", want: true},
		{name: "dev accepts medium", strategy: dev, candidate: "medium", want: true},
		{name: "dev rejects empty", strategy: dev, candidate: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.strategy.ConfidenceMeets(tc.candidate); got != tc.want {
				t.Errorf("ConfidenceMeets(%q) under %s = %v, want %v", tc.candidate, tc.strategy.MinConfidence, got, tc.want)
			}
		})
	}
}

func TestSizing_MemoryCutAllowed(t *testing.T) {
	prod := For(environment.EnvProd)
	staging := For(environment.EnvStaging)
	dev := For(environment.EnvDev)

	for _, tc := range []struct {
		name     string
		strategy Sizing
		pct      int
		want     bool
	}{
		{name: "prod 5% allowed", strategy: prod, pct: 5, want: true},
		{name: "prod 10% boundary allowed", strategy: prod, pct: 10, want: true},
		{name: "prod 15% rejected", strategy: prod, pct: 15, want: false},
		{name: "staging 25% boundary allowed", strategy: staging, pct: 25, want: true},
		{name: "staging 30% rejected", strategy: staging, pct: 30, want: false},
		{name: "dev allows large cut under 100", strategy: dev, pct: 80, want: true},
		{name: "dev allows boundary 100", strategy: dev, pct: 100, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.strategy.MemoryCutAllowed(tc.pct); got != tc.want {
				t.Errorf("MemoryCutAllowed(%d) under cap=%d = %v, want %v", tc.pct, tc.strategy.MaxMemoryReductionPct, got, tc.want)
			}
		})
	}
}

func TestSizing_ReplicaCutAllowed(t *testing.T) {
	prod := For(environment.EnvProd)
	staging := For(environment.EnvStaging)
	dev := For(environment.EnvDev)

	for _, tc := range []struct {
		name     string
		strategy Sizing
		count    int
		want     bool
	}{
		{name: "prod rejects 1 replica cut", strategy: prod, count: 1, want: false},
		{name: "prod accepts zero", strategy: prod, count: 0, want: true},
		{name: "staging accepts 1", strategy: staging, count: 1, want: true},
		{name: "staging rejects 3", strategy: staging, count: 3, want: false},
		{name: "dev accepts 50", strategy: dev, count: 50, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.strategy.ReplicaCutAllowed(tc.count); got != tc.want {
				t.Errorf("ReplicaCutAllowed(%d) under cap=%d = %v, want %v", tc.count, tc.strategy.MaxReplicaReductionPerPR, got, tc.want)
			}
		})
	}
}
