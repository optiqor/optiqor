// Package gate runs a chained validation pipeline on an Apply Fix
// candidate before it leaves the worker. Wired in Phase 3 with stub
// validators so dispatch can't bypass it later; real stages land
// Phase 4 (template, conform, post) and Phase 5 (dryrun).
package gate

import (
	"context"
	"errors"
	"fmt"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Stage names the validator slot. Pipeline runs them in declaration
// order and short-circuits on the first stage Policy disallows.
type Stage string

const (
	StageTemplate Stage = "template"
	StageConform  Stage = "conform"
	StageDryrun   Stage = "dryrun"
	StagePost     Stage = "post"
)

// Status distinguishes NotImplemented from Failed so SkeletonPolicy can
// allow stub stages through while StrictPolicy blocks them.
type Status int

const (
	StatusPassed Status = iota
	StatusFailed
	StatusNotImplemented
)

func (s Status) String() string {
	switch s {
	case StatusPassed:
		return "passed"
	case StatusFailed:
		return "failed"
	case StatusNotImplemented:
		return "not_implemented"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

type Candidate struct {
	ApplyFixID  string
	ChartYAML   string
	UnifiedDiff string
	Workload    string
}

type StageResult struct {
	Stage  Stage
	Status Status
	Detail string
	Err    error
}

type Result struct {
	Stages []StageResult
}

type Validator interface {
	Stage() Stage
	Validate(ctx context.Context, t tenancy.Context, c Candidate) StageResult
}

type Policy interface {
	Allow(stage Stage, status Status) bool
}

type Pipeline struct {
	validators []Validator
	policy     Policy
}

// NewPipeline panics on nil policy or zero validators so a misconfigured
// boot fails closed instead of dispatching unvalidated PRs.
func NewPipeline(policy Policy, validators ...Validator) *Pipeline {
	if policy == nil {
		panic("gate: NewPipeline: nil policy")
	}
	if len(validators) == 0 {
		panic("gate: NewPipeline: no validators")
	}
	return &Pipeline{validators: validators, policy: policy}
}

var ErrNotImplemented = errors.New("gate: validator not implemented")

func (p *Pipeline) Run(ctx context.Context, t tenancy.Context, c Candidate) (Result, error) {
	out := Result{Stages: make([]StageResult, 0, len(p.validators))}
	for _, v := range p.validators {
		res := v.Validate(ctx, t, c)
		res.Stage = v.Stage()
		out.Stages = append(out.Stages, res)
		if !p.policy.Allow(res.Stage, res.Status) {
			return out, fmt.Errorf("gate: stage %s %s: %w", res.Stage, res.Status, errOrSentinel(res))
		}
	}
	return out, nil
}

func errOrSentinel(r StageResult) error {
	if r.Err != nil {
		return r.Err
	}
	if r.Status == StatusNotImplemented {
		return ErrNotImplemented
	}
	return errors.New(r.Detail)
}
