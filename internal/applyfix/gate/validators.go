package gate

import (
	"context"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// NotImplementedValidator is the Phase-3 stand-in for every stage.
// SkeletonPolicy lets it through; StrictPolicy blocks.
type NotImplementedValidator struct{ S Stage }

func (n NotImplementedValidator) Stage() Stage { return n.S }

func (n NotImplementedValidator) Validate(_ context.Context, _ tenancy.Context, _ Candidate) StageResult {
	return StageResult{
		Stage:  n.S,
		Status: StatusNotImplemented,
		Detail: "stage not yet implemented",
		Err:    ErrNotImplemented,
	}
}

func SkeletonValidators() []Validator {
	return []Validator{
		NotImplementedValidator{S: StageTemplate},
		NotImplementedValidator{S: StageConform},
		NotImplementedValidator{S: StageDryrun},
		NotImplementedValidator{S: StagePost},
	}
}
