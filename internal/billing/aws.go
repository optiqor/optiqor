package billing

import (
	"context"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// AWSCUR locks in the registration shape now so domain code can target
// it; Athena query path lands Phase 6.
type AWSCUR struct{}

func NewAWSCUR() *AWSCUR { return &AWSCUR{} }

func (*AWSCUR) Name() string { return "aws-cur" }
func (*AWSCUR) Cloud() Cloud { return CloudAWS }
func (*AWSCUR) Tier() Tier   { return TierCloud }

func (*AWSCUR) Query(_ context.Context, t tenancy.Context, w Window) (Result, error) {
	if err := t.Validate(); err != nil {
		return Result{}, err
	}
	if err := w.Validate(); err != nil {
		return Result{}, err
	}
	return Result{}, ErrNotImplemented
}
