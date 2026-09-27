package environment

import "testing"

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		name          string
		clusterName   string
		clusterLabels map[string]string
		nsLabels      map[string]string
		ns            string
		customRules   map[string]Environment
		want          Environment
	}{
		{
			name:          "cluster-label-beats-name",
			clusterName:   "acme-prod-1",
			clusterLabels: map[string]string{"environment": "dev"},
			ns:            "ns",
			want:          EnvDev,
		},
		{
			name:        "namespace-label-overrides-cluster-name",
			clusterName: "acme-prod-1",
			nsLabels:    map[string]string{"environment": "staging"},
			ns:          "ns",
			want:        EnvStaging,
		},
		{
			name:          "custom-rule-wins",
			clusterName:   "acme-prod-1",
			clusterLabels: map[string]string{"environment": "dev"},
			ns:            "payments-prod",
			customRules:   map[string]Environment{"payments-prod": EnvProd},
			want:          EnvProd,
		},
		{name: "cluster-name-prod-suffix", clusterName: "acme-prod", ns: "ns", want: EnvProd},
		{name: "cluster-name-production-prefix", clusterName: "production-cluster", ns: "ns", want: EnvProd},
		{name: "cluster-name-prod-prefix", clusterName: "prod-eu", ns: "ns", want: EnvProd},
		{name: "cluster-name-staging-suffix", clusterName: "acme-staging", ns: "ns", want: EnvStaging},
		{name: "cluster-name-stg-suffix", clusterName: "acme-stg", ns: "ns", want: EnvStaging},
		{name: "cluster-name-staging-prefix", clusterName: "staging-cluster", ns: "ns", want: EnvStaging},
		{name: "cluster-name-dev-suffix", clusterName: "acme-dev", ns: "ns", want: EnvDev},
		{name: "cluster-name-dev-prefix", clusterName: "dev-eu", ns: "ns", want: EnvDev},
		{name: "no-signal-falls-to-unknown", clusterName: "random-name", ns: "ns", want: EnvUnknown},
		{name: "empty-everything-unknown", clusterName: "c1", ns: "ns", want: EnvUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.clusterName, tc.clusterLabels, tc.nsLabels, tc.ns, tc.customRules)
			if got != tc.want {
				t.Errorf("Classify = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProfileFor(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		in                    Environment
		wantEnv               Environment
		wantAggressiveness    Aggressiveness
		wantAutoMergeEligible bool
		wantManualApproval    bool
		wantConfidenceFloor   string
	}{
		{
			// Fail-safe: unknown maps to prod profile. See env doc comment.
			name:                "unknown-fails-safe-to-prod",
			in:                  EnvUnknown,
			wantEnv:             EnvProd,
			wantAggressiveness:  AggressivenessConservative,
			wantManualApproval:  true,
			wantConfidenceFloor: "high",
		},
		{
			name:                  "dev-aggressive-auto-merge",
			in:                    EnvDev,
			wantEnv:               EnvDev,
			wantAggressiveness:    AggressivenessAggressive,
			wantAutoMergeEligible: true,
			wantConfidenceFloor:   "low",
		},
		{
			// Phase 4 may revisit the no-manual-approval call.
			name:                "staging-medium-no-auto-merge",
			in:                  EnvStaging,
			wantEnv:             EnvStaging,
			wantAggressiveness:  AggressivenessMedium,
			wantConfidenceFloor: "medium",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := ProfileFor(tc.in)
			if p.Environment != tc.wantEnv {
				t.Errorf("Environment = %v, want %v", p.Environment, tc.wantEnv)
			}
			if p.Aggressiveness != tc.wantAggressiveness {
				t.Errorf("Aggressiveness = %v, want %v", p.Aggressiveness, tc.wantAggressiveness)
			}
			if p.AutoMergeEligible != tc.wantAutoMergeEligible {
				t.Errorf("AutoMergeEligible = %v, want %v", p.AutoMergeEligible, tc.wantAutoMergeEligible)
			}
			if p.ManualApproval != tc.wantManualApproval {
				t.Errorf("ManualApproval = %v, want %v", p.ManualApproval, tc.wantManualApproval)
			}
			if p.ConfidenceFloor != tc.wantConfidenceFloor {
				t.Errorf("ConfidenceFloor = %q, want %q", p.ConfidenceFloor, tc.wantConfidenceFloor)
			}
		})
	}
}

func TestCanonical(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want Environment
	}{
		{name: "prod", in: "prod", want: EnvProd},
		{name: "production-uppercase", in: "PRODUCTION", want: EnvProd},
		{name: "staging-whitespace", in: "  staging  ", want: EnvStaging},
		{name: "stg", in: "stg", want: EnvStaging},
		{name: "dev", in: "dev", want: EnvDev},
		{name: "qa-not-canonical", in: "qa", want: EnvUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := canonical(tc.in); got != tc.want {
				t.Errorf("canonical(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
