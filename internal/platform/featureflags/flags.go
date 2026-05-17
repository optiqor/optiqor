// Package featureflags wraps the OpenFeature client (backed by
// self-hosted Unleash in production) with a tenant-aware evaluation
// context.
//
// Phase 1 ships an in-process Provider that returns deterministic
// defaults so domain code can be written against the interface today.
// The Unleash adapter swaps in via Set() when the flag service comes
// online — call sites do not change.
package featureflags

import (
	"context"
	"sync"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Provider is what every feature-flag backend implements. The
// signatures intentionally mirror the OpenFeature SDK so a real
// adapter is one type alias away.
type Provider interface {
	BoolValue(ctx context.Context, flag string, defaultValue bool, eval EvalContext) bool
	StringValue(ctx context.Context, flag string, defaultValue string, eval EvalContext) string
	NumberValue(ctx context.Context, flag string, defaultValue float64, eval EvalContext) float64
}

// EvalContext is the targeting context. Tenant-aware features key on
// TenantID; per-workspace overrides use WorkspaceID.
type EvalContext struct {
	TenantID    string
	WorkspaceID string
	UserID      string
	// Custom is opaque per-flag attribute bag — e.g. the cluster's
	// node-provisioner class for a feature gated on Karpenter.
	Custom map[string]any
}

// EvalFromTenant builds an EvalContext from a tenancy.Context.
func EvalFromTenant(t tenancy.Context) EvalContext {
	return EvalContext{TenantID: t.TenantID, WorkspaceID: t.WorkspaceID}
}

// Client is what callers use. One per process; wraps a Provider so the
// Provider can be swapped at runtime without rebuilding the dep graph.
type Client struct {
	mu       sync.RWMutex
	provider Provider
}

// NewClient returns a Client backed by p. Pass NoopProvider() during
// tests or when the flag service is unavailable.
func NewClient(p Provider) *Client {
	return &Client{provider: p}
}

// Set replaces the underlying Provider. Safe to call at any time;
// subsequent reads use the new provider. Used at boot to swap in the
// Unleash adapter once the connection is up.
func (c *Client) Set(p Provider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.provider = p
}

// Bool evaluates a boolean flag.
func (c *Client) Bool(ctx context.Context, flag string, def bool, eval EvalContext) bool {
	c.mu.RLock()
	p := c.provider
	c.mu.RUnlock()
	if p == nil {
		return def
	}
	return p.BoolValue(ctx, flag, def, eval)
}

// String evaluates a string flag.
func (c *Client) String(ctx context.Context, flag, def string, eval EvalContext) string {
	c.mu.RLock()
	p := c.provider
	c.mu.RUnlock()
	if p == nil {
		return def
	}
	return p.StringValue(ctx, flag, def, eval)
}

// Number evaluates a numeric flag.
func (c *Client) Number(ctx context.Context, flag string, def float64, eval EvalContext) float64 {
	c.mu.RLock()
	p := c.provider
	c.mu.RUnlock()
	if p == nil {
		return def
	}
	return p.NumberValue(ctx, flag, def, eval)
}

// NoopProvider always returns the supplied defaults. Safe production
// fallback when the flag service is unreachable.
func NoopProvider() Provider { return noopProvider{} }

type noopProvider struct{}

func (noopProvider) BoolValue(_ context.Context, _ string, def bool, _ EvalContext) bool {
	return def
}

func (noopProvider) StringValue(_ context.Context, _, def string, _ EvalContext) string {
	return def
}

func (noopProvider) NumberValue(_ context.Context, _ string, def float64, _ EvalContext) float64 {
	return def
}

// StaticProvider is a deterministic in-memory provider. Useful in
// tests and for canary-deployment seed values; not suitable for
// production where Unleash drives flags.
type StaticProvider struct {
	Bools   map[string]bool
	Strings map[string]string
	Numbers map[string]float64
}

// NewStaticProvider returns an empty StaticProvider; callers populate
// the maps directly before handing it to NewClient.
func NewStaticProvider() *StaticProvider {
	return &StaticProvider{
		Bools:   map[string]bool{},
		Strings: map[string]string{},
		Numbers: map[string]float64{},
	}
}

func (s *StaticProvider) BoolValue(_ context.Context, flag string, def bool, _ EvalContext) bool {
	if v, ok := s.Bools[flag]; ok {
		return v
	}
	return def
}

func (s *StaticProvider) StringValue(_ context.Context, flag, def string, _ EvalContext) string {
	if v, ok := s.Strings[flag]; ok {
		return v
	}
	return def
}

func (s *StaticProvider) NumberValue(_ context.Context, flag string, def float64, _ EvalContext) float64 {
	if v, ok := s.Numbers[flag]; ok {
		return v
	}
	return def
}
