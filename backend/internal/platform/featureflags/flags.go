package featureflags

import (
	"context"
	"sync"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Provider mirrors the OpenFeature SDK signatures so a real adapter is
// one type alias away.
type Provider interface {
	BoolValue(ctx context.Context, flag string, defaultValue bool, eval EvalContext) bool
	StringValue(ctx context.Context, flag string, defaultValue string, eval EvalContext) string
	NumberValue(ctx context.Context, flag string, defaultValue float64, eval EvalContext) float64
}

// EvalContext is the targeting context. Tenant features key on
// TenantID; per-workspace overrides use WorkspaceID. Custom carries
// per-flag attributes (e.g. node-provisioner class for a Karpenter gate).
type EvalContext struct {
	TenantID    string
	WorkspaceID string
	UserID      string
	Custom      map[string]any
}

func EvalFromTenant(t tenancy.Context) EvalContext {
	return EvalContext{TenantID: t.TenantID, WorkspaceID: t.WorkspaceID}
}

// Client wraps a Provider so the backend can be swapped at runtime
// without rebuilding the dep graph. One per process.
type Client struct {
	mu       sync.RWMutex
	provider Provider
}

// NewClient backs the Client with p. Pass NoopProvider() in tests or
// when the flag service is unavailable.
func NewClient(p Provider) *Client {
	return &Client{provider: p}
}

// Set swaps the Provider at runtime. Used at boot to install the
// Unleash adapter once its connection is up.
func (c *Client) Set(p Provider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.provider = p
}

func (c *Client) Bool(ctx context.Context, flag string, def bool, eval EvalContext) bool {
	c.mu.RLock()
	p := c.provider
	c.mu.RUnlock()
	if p == nil {
		return def
	}
	return p.BoolValue(ctx, flag, def, eval)
}

func (c *Client) String(ctx context.Context, flag, def string, eval EvalContext) string {
	c.mu.RLock()
	p := c.provider
	c.mu.RUnlock()
	if p == nil {
		return def
	}
	return p.StringValue(ctx, flag, def, eval)
}

func (c *Client) Number(ctx context.Context, flag string, def float64, eval EvalContext) float64 {
	c.mu.RLock()
	p := c.provider
	c.mu.RUnlock()
	if p == nil {
		return def
	}
	return p.NumberValue(ctx, flag, def, eval)
}

// NoopProvider returns supplied defaults. Safe fallback when the flag
// service is unreachable.
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

// StaticProvider is an in-memory provider for tests and canary seed
// values. Not for production — Unleash drives flags there.
type StaticProvider struct {
	Bools   map[string]bool
	Strings map[string]string
	Numbers map[string]float64
}

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
