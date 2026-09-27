//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/validator"
)

// TestValidator_DefaultPipeline_Composition verifies the six Phase-4
// validators run in declaration order and the right one rejects per
// signal. Catches drift in Default() ordering or new validators that
// forget to set Verdict.Validator.
func TestValidator_DefaultPipeline_Composition(t *testing.T) {
	ctx := context.Background()
	tnt := tenancy.Context{TenantID: "tenant-int"}

	for _, tc := range []struct {
		name       string
		cand       validator.Candidate
		want       string // first hard-reject reason substring (empty = accept)
		validators []string
	}{
		{
			name: "clean candidate dispatches",
			cand: validator.Candidate{ProposedReplicas: 3},
		},
		{
			name: "PDB minAvailable trumps later stages",
			cand: validator.Candidate{
				ProposedReplicas: 1,
				ProposedMemory:   validator.Quantity{Bytes: 1 << 30},
				Signals: validator.ClusterSignals{
					PDB:       &validator.PDB{MinAvailable: 2},
					OOMRecent: true,
				},
			},
			want:       "PDB minAvailable",
			validators: []string{"pdb"},
		},
		{
			name: "OOMRecent blocks memory cut after pdb passes",
			cand: validator.Candidate{
				ProposedMemory: validator.Quantity{Bytes: 1 << 30},
				Signals:        validator.ClusterSignals{OOMRecent: true},
			},
			want:       "OOMKill",
			validators: []string{"oom-recent"},
		},
		{
			name: "HPA in-bounds warns but doesn't reject",
			cand: validator.Candidate{
				ProposedReplicas: 5,
				Signals:          validator.ClusterSignals{HPA: &validator.HPA{MinReplicas: 2, MaxReplicas: 10}},
			},
			validators: []string{"hpabounds"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := validator.NewPipeline(validator.Default()...)
			res, err := p.Run(ctx, tnt, tc.cand)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if tc.want == "" {
				if res.Rejected != nil {
					t.Errorf("expected accept, got %+v", res.Rejected)
				}
			} else {
				if res.Rejected == nil || !strings.Contains(res.Rejected.Reason, tc.want) {
					t.Errorf("rejection = %+v, want substring %q", res.Rejected, tc.want)
				}
			}
			for _, want := range tc.validators {
				found := false
				for _, v := range res.Verdicts {
					if v.Validator == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("verdicts %+v missing validator %q", res.Verdicts, want)
				}
			}
		})
	}
}
