package billing

import (
	"context"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Capacity backs the Capacity Receipt tier (bare-metal, on-prem,
// Hetzner flat-rate) where savings are freed cores + GiB and the USD
// conversion uses customer CapEx/depreciation, not a public price book.
type Capacity struct{}

func NewCapacity() *Capacity { return &Capacity{} }

func (*Capacity) Name() string { return "capacity" }
func (*Capacity) Cloud() Cloud { return Cloud("") }
func (*Capacity) Tier() Tier   { return TierCapacity }

func (*Capacity) Query(_ context.Context, t tenancy.Context, w Window) (Result, error) {
	if err := t.Validate(); err != nil {
		return Result{}, err
	}
	if err := w.Validate(); err != nil {
		return Result{}, err
	}
	return Result{}, ErrNotImplemented
}
