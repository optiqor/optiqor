package billing

import (
	"context"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// AWSCUR is the AWS Cost and Usage Report source. Phase 6 fills in
// the Athena query path; the type and registration shape lock in now
// so domain code can be written against it.
type AWSCUR struct{}

// NewAWSCUR returns a registered-by-default AWS CUR source.
func NewAWSCUR() *AWSCUR { return &AWSCUR{} }

func (*AWSCUR) Name() string { return "aws-cur" }
func (*AWSCUR) Cloud() Cloud { return CloudAWS }
func (*AWSCUR) Tier() Tier   { return TierCloud }

// Query reads CUR via Athena. Phase 1 returns ErrNotImplemented; the
// shape is fixed so callers can wire a fake in tests.
func (*AWSCUR) Query(_ context.Context, t tenancy.Context, w Window) (Result, error) {
	if err := t.Validate(); err != nil {
		return Result{}, err
	}
	if err := w.Validate(); err != nil {
		return Result{}, err
	}
	return Result{}, ErrNotImplemented
}
