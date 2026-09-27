package gate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestPipeline_Run(t *testing.T) {
	ctx := context.Background()
	tc := tenancy.Context{TenantID: "00000000-0000-0000-0000-000000000001"}
	cand := Candidate{ApplyFixID: "af-1", Workload: "api"}

	for _, tt := range []struct {
		name       string
		policy     Policy
		validators []Validator
		wantErr    bool
		wantStages int
		errSub     string
	}{
		{
			name:       "skeleton policy: all not-implemented passes",
			policy:     SkeletonPolicy{},
			validators: SkeletonValidators(),
			wantErr:    false,
			wantStages: 4,
		},
		{
			name:   "strict policy: not-implemented blocks at first stage",
			policy: StrictPolicy{},
			validators: []Validator{
				NotImplementedValidator{S: StageTemplate},
				NotImplementedValidator{S: StageConform},
			},
			wantErr:    true,
			wantStages: 1,
			errSub:     "template not_implemented",
		},
		{
			name:   "skeleton policy: explicit failure short-circuits",
			policy: SkeletonPolicy{},
			validators: []Validator{
				NotImplementedValidator{S: StageTemplate},
				stubValidator{stage: StageConform, status: StatusFailed, detail: "kubeconform: missing api"},
				NotImplementedValidator{S: StageDryrun},
			},
			wantErr:    true,
			wantStages: 2,
			errSub:     "conform failed",
		},
		{
			name:   "strict policy: all-passing runs every stage",
			policy: StrictPolicy{},
			validators: []Validator{
				stubValidator{stage: StageTemplate, status: StatusPassed},
				stubValidator{stage: StageConform, status: StatusPassed},
				stubValidator{stage: StageDryrun, status: StatusPassed},
				stubValidator{stage: StagePost, status: StatusPassed},
			},
			wantErr:    false,
			wantStages: 4,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPipeline(tt.policy, tt.validators...)
			res, err := p.Run(ctx, tc, cand)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if len(res.Stages) != tt.wantStages {
				t.Fatalf("stages run = %d, want %d", len(res.Stages), tt.wantStages)
			}
			if tt.errSub != "" && !strings.Contains(err.Error(), tt.errSub) {
				t.Errorf("err %q missing %q", err.Error(), tt.errSub)
			}
		})
	}
}

func TestNewPipeline_Panics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy Policy
		vals   []Validator
		want   string
	}{
		{name: "nil policy", policy: nil, vals: []Validator{NotImplementedValidator{S: StageTemplate}}, want: "nil policy"},
		{name: "no validators", policy: SkeletonPolicy{}, vals: nil, want: "no validators"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected panic")
				}
				if !strings.Contains(r.(string), tc.want) {
					t.Errorf("panic %q missing %q", r, tc.want)
				}
			}()
			NewPipeline(tc.policy, tc.vals...)
		})
	}
}

func TestPipeline_RunWrapsErrNotImplemented(t *testing.T) {
	p := NewPipeline(StrictPolicy{}, NotImplementedValidator{S: StageTemplate})
	_, err := p.Run(context.Background(), tenancy.Context{TenantID: "t"}, Candidate{})
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("err %v should wrap ErrNotImplemented", err)
	}
}

type stubValidator struct {
	stage  Stage
	status Status
	detail string
}

func (s stubValidator) Stage() Stage { return s.stage }
func (s stubValidator) Validate(_ context.Context, _ tenancy.Context, _ Candidate) StageResult {
	return StageResult{Stage: s.stage, Status: s.status, Detail: s.detail}
}
