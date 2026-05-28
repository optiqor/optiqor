package strategy

import (
	"testing"

	"github.com/optiqor/optiqor/internal/agent/provisioner"
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
				MinConfidence: "high", MaxAggressiveness: 100, ManualApproval: true,
			},
		},
		{
			env: environment.EnvStaging,
			want: Sizing{
				PercentileTarget: 95, MaxMemoryReductionPct: 25, MaxReplicaReductionPerPR: 2,
				MinConfidence: "medium", MaxAggressiveness: 100,
			},
		},
		{
			env: environment.EnvDev,
			want: Sizing{
				PercentileTarget: 95, MaxMemoryReductionPct: 0, MaxReplicaReductionPerPR: 100,
				MinConfidence: "low", MaxAggressiveness: 100, AutoMergeEligible: true,
			},
		},
		{
			env: environment.EnvUnknown,
			want: Sizing{
				PercentileTarget: 99, MaxMemoryReductionPct: 10, MaxReplicaReductionPerPR: 0,
				MinConfidence: "high", MaxAggressiveness: 100, ManualApproval: true,
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
		{name: "dev no-cap rejects positive", strategy: dev, pct: 50, want: false},
		{name: "dev no-cap allows zero", strategy: dev, pct: 0, want: true},
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

func TestForClass_StaticCapsAggressiveness(t *testing.T) {
	for _, tc := range []struct {
		name              string
		env               environment.Environment
		class             provisioner.Class
		wantAggressive    int
		wantMinConfidence string
		wantReplicaCap    int
	}{
		{"dev karpenter is full aggressive", environment.EnvDev, provisioner.ClassKarpenter, 100, "low", 100},
		{"dev autoscaler is full aggressive", environment.EnvDev, provisioner.ClassAutoscaler, 100, "low", 100},
		{"dev static caps replicas and lifts confidence", environment.EnvDev, provisioner.ClassStatic, 50, "medium", 0},
		{"prod karpenter stays prod-strict", environment.EnvProd, provisioner.ClassKarpenter, 100, "high", 0},
		{"prod static stays prod-strict", environment.EnvProd, provisioner.ClassStatic, 50, "high", 0},
		{"staging static lifts confidence", environment.EnvStaging, provisioner.ClassStatic, 50, "medium", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ForClass(tc.env, tc.class)
			if s.MaxAggressiveness != tc.wantAggressive {
				t.Errorf("MaxAggressiveness = %d, want %d", s.MaxAggressiveness, tc.wantAggressive)
			}
			if s.MinConfidence != tc.wantMinConfidence {
				t.Errorf("MinConfidence = %q, want %q", s.MinConfidence, tc.wantMinConfidence)
			}
			if s.MaxReplicaReductionPerPR != tc.wantReplicaCap {
				t.Errorf("MaxReplicaReductionPerPR = %d, want %d", s.MaxReplicaReductionPerPR, tc.wantReplicaCap)
			}
		})
	}
}

func TestSizing_EffectiveCaps_ScaleByAggressiveness(t *testing.T) {
	for _, tc := range []struct {
		name     string
		s        Sizing
		wantMem  int
		wantReps int
	}{
		{"full agg keeps base", Sizing{MaxMemoryReductionPct: 25, MaxReplicaReductionPerPR: 4, MaxAggressiveness: 100}, 25, 4},
		{"half agg halves base", Sizing{MaxMemoryReductionPct: 30, MaxReplicaReductionPerPR: 4, MaxAggressiveness: 50}, 15, 2},
		{"zero agg zeroes base", Sizing{MaxMemoryReductionPct: 30, MaxReplicaReductionPerPR: 4, MaxAggressiveness: 0}, 0, 0},
		{"unset agg zeroes base", Sizing{MaxMemoryReductionPct: 30, MaxReplicaReductionPerPR: 4}, 0, 0},
		{"agg over 100 caps at base", Sizing{MaxMemoryReductionPct: 10, MaxReplicaReductionPerPR: 1, MaxAggressiveness: 200}, 10, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.EffectiveMemoryCap(); got != tc.wantMem {
				t.Errorf("EffectiveMemoryCap = %d, want %d", got, tc.wantMem)
			}
			if got := tc.s.EffectiveReplicaCap(); got != tc.wantReps {
				t.Errorf("EffectiveReplicaCap = %d, want %d", got, tc.wantReps)
			}
		})
	}
}

func TestSizing_MemoryCutAllowed_ConsumesAggressiveness(t *testing.T) {
	full := Sizing{MaxMemoryReductionPct: 20, MaxAggressiveness: 100}
	half := Sizing{MaxMemoryReductionPct: 20, MaxAggressiveness: 50}
	if !full.MemoryCutAllowed(15) {
		t.Error("full agg 15%% should be allowed (cap=20)")
	}
	if half.MemoryCutAllowed(15) {
		t.Error("half agg 15%% should be rejected (effective cap=10)")
	}
	if !half.MemoryCutAllowed(10) {
		t.Error("half agg 10%% boundary should be allowed")
	}
}

func TestFor_BackCompatDefaultsToKarpenter(t *testing.T) {
	// The single-arg shape must equal ForClass(env, ClassKarpenter) so
	// pre-Phase-5 call sites stay byte-stable.
	for _, env := range []environment.Environment{
		environment.EnvProd, environment.EnvStaging, environment.EnvDev, environment.EnvUnknown,
	} {
		if For(env) != ForClass(env, provisioner.ClassKarpenter) {
			t.Errorf("For(%q) != ForClass(env, Karpenter) — back-compat broken", env)
		}
	}
}
