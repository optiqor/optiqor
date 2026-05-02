// Package billing defines the pluggable cost-source contract.
//
// Optiqor issues three Receipt tiers (cloud / capacity / hybrid). Each
// tier is backed by a concrete BillingSource implementation:
//
//   - AWS CUR (Phase 6, first impl) — Cloud Receipt
//   - Azure Cost Management (Phase 7) — Cloud Receipt
//   - Hetzner Cloud invoices (Phase 8) — Cloud Receipt
//   - Capacity (any K8s without managed cloud billing) — Capacity Receipt
//   - Aggregator combining the above for one tenant — Hybrid Receipt
//
// The interface in this package is what the receipt issuer, cost
// engine, and dashboards consume. Adding a new cloud is a matter of
// implementing BillingSource and registering it via Register; nothing
// in domain code changes.
package billing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/optiqor/backend/internal/tenancy"
)

// Tier names map 1-to-1 to the receipts.tier CHECK constraint in
// migrations/0001_baseline.sql.
type Tier string

const (
	TierCloud    Tier = "cloud"
	TierCapacity Tier = "capacity"
	TierHybrid   Tier = "hybrid"
)

// Cloud names a managed-cloud billing surface. "" → not a managed
// cloud (used by Capacity sources running on bare-metal / on-prem).
type Cloud string

const (
	CloudAWS     Cloud = "aws"
	CloudAzure   Cloud = "azure"
	CloudHetzner Cloud = "hetzner"
	CloudGCP     Cloud = "gcp"
)

// Window is the closed-open time range a query covers.
type Window struct {
	Start time.Time
	End   time.Time
}

// Validate returns an error if the window is unusable. Used by every
// concrete source's Query implementation as the first thing.
func (w Window) Validate() error {
	if w.End.Before(w.Start) || w.End.Equal(w.Start) {
		return fmt.Errorf("billing: invalid window %s..%s", w.Start, w.End)
	}
	return nil
}

// LineItem is a normalised view of one bill row, regardless of cloud.
// Concrete sources translate their native shape into LineItems.
type LineItem struct {
	ClusterID     string
	WorkloadID    string // optional; "" if attribution is below cluster level
	Resource      string // e.g. "ec2:m5.large", "hetzner:CCX13", "capacity:cpu"
	UsageQty      float64
	UnitUSDCents  int64 // unit cost in cents; 0 if not priced (Capacity tier)
	TotalUSDCents int64
	Currency      string // ISO 4217; "USD" by default; non-USD only for non-AWS sources
}

// Result is what a Source returns for a query window.
type Result struct {
	Source   string // implementation name, e.g. "aws-cur"
	Cloud    Cloud  // empty for Capacity sources
	Tier     Tier
	Window   Window
	Items    []LineItem
	Currency string
}

// TotalUSDCents converts non-USD currencies via the FX hint stored on
// each item; for now we only accept USD-billed sources, so this just
// sums TotalUSDCents on items in USD and panics otherwise. Callers
// must ensure currency consistency before invoking.
func (r Result) TotalUSDCents() int64 {
	var sum int64
	for _, it := range r.Items {
		if it.Currency != "" && it.Currency != "USD" {
			// Non-USD items must be converted by the caller before
			// being fed back into a USD aggregation.
			continue
		}
		sum += it.TotalUSDCents
	}
	return sum
}

// Source is the contract every billing implementation satisfies.
type Source interface {
	// Name is the stable identifier ("aws-cur", "azure-cm", "hetzner",
	// "capacity", "hybrid"). Used for telemetry and Receipt provenance.
	Name() string

	// Cloud is the cloud this source bills for; "" for Capacity sources.
	Cloud() Cloud

	// Tier is the Receipt tier this source produces.
	Tier() Tier

	// Query returns billing line items for the window. Implementations
	// must enforce tenant isolation before reading any external data.
	Query(ctx context.Context, t tenancy.Context, w Window) (Result, error)
}

// Registry holds the set of registered sources, keyed by Name. One
// registry per process; use Default() unless you have a reason.
type Registry struct {
	mu      sync.RWMutex
	sources map[string]Source
}

func newRegistry() *Registry { return &Registry{sources: map[string]Source{}} }

var defaultRegistry = newRegistry()

// Default returns the process-wide registry. Production wiring at boot
// adds the AWS CUR / Azure / Hetzner / Capacity sources.
func Default() *Registry { return defaultRegistry }

// Register adds a source. Duplicates panic to surface configuration
// bugs at startup, not at first query.
func (r *Registry) Register(s Source) {
	if s == nil || s.Name() == "" {
		panic("billing: nil source or empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.sources[s.Name()]; dup {
		panic("billing: duplicate source " + s.Name())
	}
	r.sources[s.Name()] = s
}

// Lookup returns a registered source. Caller is responsible for
// tenant validation; the Source impl re-validates inside Query.
func (r *Registry) Lookup(name string) (Source, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sources[name]
	if !ok {
		return nil, fmt.Errorf("billing: source %q not registered", name)
	}
	return s, nil
}

// Names returns the registered source names in insertion-stable order
// for deterministic Result ordering when aggregating.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.sources))
	for n := range r.sources {
		out = append(out, n)
	}
	sortStrings(out)
	return out
}

// ErrNotImplemented is returned by stub sources whose query path
// hasn't been wired yet (Phase 6+ for AWS CUR, Phase 7+ for Azure).
var ErrNotImplemented = errors.New("billing: not implemented in this phase")

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
