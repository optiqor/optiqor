package tenancy

import (
	"context"
	"errors"
	"fmt"
)

// ErrNoTenant signals a programming error: domain code must always
// run inside a tenant scope.
var ErrNoTenant = errors.New("tenancy: no tenant in context")

// Context is the tenant scope for a request or workflow. Per CLAUDE.md,
// every domain package's public method takes this as the first arg
// after context.Context — no exceptions; RLS enforcement on the DB
// side assumes it.
//
// Schema hierarchy: tenant → workspace → cluster → namespace →
// workload. TenantID is mandatory; the others narrow scope when set,
// empty meaning "all" at that level.
type Context struct {
	TenantID    string
	WorkspaceID string
	ClusterID   string
	Namespace   string
}

// String is the stable, log-safe representation used in slog attrs.
func (c Context) String() string {
	return fmt.Sprintf("tenant=%s workspace=%s cluster=%s ns=%s",
		c.TenantID, c.WorkspaceID, c.ClusterID, c.Namespace)
}

func (c Context) Validate() error {
	if c.TenantID == "" {
		return ErrNoTenant
	}
	return nil
}

type ctxKey struct{}

// WithContext attaches t at the boundary (HTTP middleware, Temporal
// workflow start) so FromContext can recover it downstream.
func WithContext(ctx context.Context, t Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, t)
}

// FromContext returns ErrNoTenant if none set or the stored value
// fails Validate.
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

// MustFromContext panics on error. Use only where the tenant scope is
// a programming invariant.
func MustFromContext(ctx context.Context) Context {
	t, err := FromContext(ctx)
	if err != nil {
		panic(err)
	}
	return t
}
