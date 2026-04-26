package environment

import "testing"

func TestClassify_LabelWins(t *testing.T) {
	got := Classify("acme-prod-1", map[string]string{"environment": "dev"}, nil, "ns", nil)
	if got != EnvDev {
		t.Errorf("label should beat name: %v", got)
	}
}

func TestClassify_NamespaceLabelOverridesClusterName(t *testing.T) {
	got := Classify("acme-prod-1", nil, map[string]string{"environment": "staging"}, "ns", nil)
	if got != EnvStaging {
		t.Errorf("namespace label should beat cluster name: %v", got)
	}
}

func TestClassify_CustomRuleWins(t *testing.T) {
	got := Classify("acme-prod-1",
		map[string]string{"environment": "dev"}, nil,
		"payments-prod",
		map[string]Environment{"payments-prod": EnvProd},
	)
	if got != EnvProd {
		t.Errorf("custom rule should win: %v", got)
	}
}

func TestClassify_ClusterNamePatterns(t *testing.T) {
	cases := map[string]Environment{
		"acme-prod":         EnvProd,
		"production-cluster": EnvProd,
		"prod-eu":           EnvProd,
		"acme-staging":      EnvStaging,
		"acme-stg":          EnvStaging,
		"staging-cluster":   EnvStaging,
		"acme-dev":          EnvDev,
		"dev-eu":            EnvDev,
		"random-name":       EnvUnknown,
	}
	for name, want := range cases {
		got := Classify(name, nil, nil, "ns", nil)
		if got != want {
			t.Errorf("Classify(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestClassify_FailSafeUnknown(t *testing.T) {
	if got := Classify("c1", nil, nil, "ns", nil); got != EnvUnknown {
		t.Errorf("Classify with no signal = %v, want unknown", got)
	}
}

func TestProfileFor_UnknownIsProd(t *testing.T) {
	p := ProfileFor(EnvUnknown)
	if p.Environment != EnvProd {
		t.Errorf("unknown should fail safe to prod, got %v", p.Environment)
	}
	if p.Aggressiveness != AggressivenessConservative {
		t.Errorf("unknown profile aggressiveness = %v, want conservative", p.Aggressiveness)
	}
	if !p.ManualApproval {
		t.Error("unknown profile must require manual approval")
	}
	if p.AutoMergeEligible {
		t.Error("unknown profile must NOT be auto-merge eligible")
	}
	if p.ConfidenceFloor != "high" {
		t.Errorf("unknown profile confidence floor = %q, want high", p.ConfidenceFloor)
	}
}

func TestProfileFor_Dev(t *testing.T) {
	p := ProfileFor(EnvDev)
	if p.Aggressiveness != AggressivenessAggressive {
		t.Errorf("dev aggressiveness = %v", p.Aggressiveness)
	}
	if !p.AutoMergeEligible {
		t.Error("dev should be auto-merge eligible")
	}
}

func TestProfileFor_Staging(t *testing.T) {
	p := ProfileFor(EnvStaging)
	if p.Aggressiveness != AggressivenessMedium {
		t.Errorf("staging aggressiveness = %v", p.Aggressiveness)
	}
	if p.AutoMergeEligible {
		t.Error("staging must not auto-merge")
	}
	if p.ManualApproval {
		t.Error("staging does not require manual approval (Phase 4 may revisit)")
	}
}

func TestCanonical(t *testing.T) {
	cases := map[string]Environment{
		"prod":        EnvProd,
		"PRODUCTION":  EnvProd,
		"  staging  ": EnvStaging,
		"stg":         EnvStaging,
		"dev":         EnvDev,
		"qa":          EnvUnknown,
	}
	for in, want := range cases {
		if got := canonical(in); got != want {
			t.Errorf("canonical(%q) = %v, want %v", in, got, want)
		}
	}
}
