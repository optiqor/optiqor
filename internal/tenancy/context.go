package tenancy

import (
	"context"
	"errors"
	"fmt"
)

// ErrNoTenant is returned when an operation requires a tenant in context but
// none is present. Callers should treat this as a programming error: domain
// code must always run inside a tenant scope.
var ErrNoTenant = errors.New("tenancy: no tenant in context")

// Context identifies the tenant scope for a request or workflow. Every domain
// package's public API takes this as the first arg after context.Context.
//
// The four-level hierarchy mirrors the Phase 1 schema:
//
//	tenant → workspace → cluster → namespace → workload
//
// TenantID is mandatory. Workspace/Cluster/Namespace are optional and narrow
// the scope further when set; an empty value means "all" at that level.
type Context struct {
	TenantID    string
	WorkspaceID string
	ClusterID   string
	Namespace   string
}

// String returns a stable, log-safe representation. Useful for slog attrs.
func (c Context) String() string {
	return fmt.Sprintf("tenant=%s workspace=%s cluster=%s ns=%s",
		c.TenantID, c.WorkspaceID, c.ClusterID, c.Namespace)
}

// Validate returns an error if the tenant scope is unusable.
func (c Context) Validate() error {
	if c.TenantID == "" {
		return ErrNoTenant
	}
	return nil
}

type ctxKey struct{}

// WithContext returns a Go context carrying t. Use this at the boundary
// (HTTP middleware, Temporal workflow start) so downstream code can recover
// the tenant scope via FromContext.
func WithContext(ctx context.Context, t Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, t)
}

// FromContext recovers the tenant scope. Returns ErrNoTenant if none set.
func FromContext(ctx context.Context) (Context, error) {
	v, ok := ctx.Value(ctxKey{}).(Context)
	if !ok {
		return Context{}, ErrNoTenant
	}
	if err := v.Validate(); err != nil {
		return Context{}, err
	}
	return v, nil
}

// MustFromContext is FromContext with panic-on-error. Use only in code paths
// where the tenant scope is a programming invariant.
func MustFromContext(ctx context.Context) Context {
	t, err := FromContext(ctx)
	if err != nil {
		panic(err)
	}
	return t
}
