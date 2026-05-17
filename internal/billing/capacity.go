package billing

import (
	"context"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Capacity sources back the Capacity Receipt tier — non-managed-cloud
// clusters (bare-metal, on-prem, Hetzner Cloud's flat-rate model) where
// "savings" are expressed as freed cores + GiB rather than dollars.
//
// The math is "freed N cores + M GiB defers next hardware purchase by Q
// quarters"; the conversion to USD value uses customer-supplied
// CapEx/depreciation parameters, not a public price book.
type Capacity struct{}

// NewCapacity returns the Capacity tier source.
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
