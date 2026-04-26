// Package db provides multi-tenant safe primitives for Postgres and Redis.
//
// The types here do not import driver packages. They define the contracts —
// tenant-scoped Postgres checkout statements and Redis key prefixing — so
// every domain package builds on safe-by-construction helpers, and the actual
// driver wiring lives in cmd/* near the application boot.
package db

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lowplane/backend/internal/tenancy"
)

// ErrEmptyTenant is returned when a Keyspace is built with no tenant ID.
var ErrEmptyTenant = errors.New("db: empty tenant id")

// Keyspace produces tenant-prefixed Redis keys. Every key returned starts
// with "t:<tenant>:" so a misconfigured query can never address another
// tenant's data.
//
// Construct one Keyspace per tenant scope (per request / per workflow).
type Keyspace struct {
	tenantID string
}

// NewKeyspace builds a Keyspace from a tenancy.Context. Returns
// ErrEmptyTenant if the tenant id is missing.
func NewKeyspace(t tenancy.Context) (Keyspace, error) {
	if t.TenantID == "" {
		return Keyspace{}, ErrEmptyTenant
	}
	return Keyspace{tenantID: t.TenantID}, nil
}

// Prefix is the literal "t:<tenant>:" used by Key/Pattern.
func (k Keyspace) Prefix() string {
	return "t:" + k.tenantID + ":"
}

// Key returns parts joined with ":" and prefixed with the tenant scope.
//
//	NewKeyspace(t).Key("rate", "ip", "1.2.3.4")  // -> "t:<tenant>:rate:ip:1.2.3.4"
func (k Keyspace) Key(parts ...string) string {
	if len(parts) == 0 {
		return k.Prefix()
	}
	return k.Prefix() + strings.Join(parts, ":")
}

// Pattern returns a tenant-scoped match pattern for SCAN.
//
//	NewKeyspace(t).Pattern("rate:*")  // -> "t:<tenant>:rate:*"
func (k Keyspace) Pattern(suffix string) string {
	return k.Prefix() + suffix
}

// TenantID exposes the underlying id. Useful for log attributes; callers
// must not use it to bypass key prefixing.
func (k Keyspace) TenantID() string {
	return k.tenantID
}

// String renders the Keyspace as its prefix, for debug logging.
func (k Keyspace) String() string {
	return fmt.Sprintf("Keyspace(%s)", k.Prefix())
}
