// Package billing is the pluggable cost-source contract behind the
// three Receipt tiers. Adding a cloud means implementing Source +
// Register; domain code stays untouched.
package billing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Tier values must match the receipts.tier CHECK in
// migrations/0001_baseline.sql.
type Tier string

const (
	TierCloud    Tier = "cloud"
	TierCapacity Tier = "capacity"
	TierHybrid   Tier = "hybrid"
)

// Cloud is empty for Capacity sources (bare-metal / on-prem).
type Cloud string

const (
	CloudAWS     Cloud = "aws"
	CloudAzure   Cloud = "azure"
	CloudHetzner Cloud = "hetzner"
	CloudGCP     Cloud = "gcp"
)

// Window is closed-open: [Start, End).
type Window struct {
	Start time.Time
	End   time.Time
}

func (w Window) Validate() error {
	if w.End.Before(w.Start) || w.End.Equal(w.Start) {
		return fmt.Errorf("billing: invalid window %s..%s", w.Start, w.End)
	}
	return nil
}

type LineItem struct {
	ClusterID     string
	WorkloadID    string // "" if attribution sits above the workload
	Resource      string // "ec2:m5.large", "hetzner:CCX13", "capacity:cpu"
	UsageQty      float64
	UnitUSDCents  int64 // 0 for Capacity tier (unpriced)
	TotalUSDCents int64
	Currency      string // ISO 4217; defaults to USD
}

type Result struct {
	Source   string // implementation name, e.g. "aws-cur"
	Cloud    Cloud  // empty for Capacity sources
	Tier     Tier
	Window   Window
	Items    []LineItem
	Currency string
}

// TotalUSDCents skips non-USD items; callers convert before aggregating.
func (r Result) TotalUSDCents() int64 {
	var sum int64
	for _, it := range r.Items {
		if it.Currency != "" && it.Currency != "USD" {
			continue
		}
		sum += it.TotalUSDCents
	}
	return sum
}

type Source interface {
	// Name is the stable id used in telemetry and Receipt provenance.
	Name() string
	// Cloud is "" for Capacity sources.
	Cloud() Cloud
	Tier() Tier
	// Query must enforce tenant isolation before any external read.
	Query(ctx context.Context, t tenancy.Context, w Window) (Result, error)
}

// Registry is process-wide; prefer Default().
type Registry struct {
	mu      sync.RWMutex
	sources map[string]Source
}

func newRegistry() *Registry { return &Registry{sources: map[string]Source{}} }

var defaultRegistry = newRegistry()

func Default() *Registry { return defaultRegistry }

// Register panics on duplicates so config bugs surface at startup.
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

// Lookup skips tenant validation; Source.Query re-validates inside.
func (r *Registry) Lookup(name string) (Source, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sources[name]
	if !ok {
		return nil, fmt.Errorf("billing: source %q not registered", name)
	}
	return s, nil
}

// Names returns sorted source names so aggregation order is deterministic.
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

// ErrNotImplemented marks stub sources (AWS CUR lands Phase 6, Azure Phase 7).
var ErrNotImplemented = errors.New("billing: not implemented in this phase")

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
