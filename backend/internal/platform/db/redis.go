package db

import (
	"errors"
	"fmt"
	"strings"

	"github.com/optiqor/optiqor/internal/tenancy"
)

var ErrEmptyTenant = errors.New("db: empty tenant id")

// Keyspace produces "t:<tenant>:" prefixed Redis keys. The prefix is
// mandatory — a key without it can address another tenant's data.
// Construct one per request / workflow.
type Keyspace struct {
	tenantID string
}

func NewKeyspace(t tenancy.Context) (Keyspace, error) {
	if t.TenantID == "" {
		return Keyspace{}, ErrEmptyTenant
	}
	return Keyspace{tenantID: t.TenantID}, nil
}

func (k Keyspace) Prefix() string {
	return "t:" + k.tenantID + ":"
}

// Key joins parts with ":" under the tenant prefix.
func (k Keyspace) Key(parts ...string) string {
	if len(parts) == 0 {
		return k.Prefix()
	}
	return k.Prefix() + strings.Join(parts, ":")
}

// Pattern returns a tenant-scoped SCAN match pattern.
func (k Keyspace) Pattern(suffix string) string {
	return k.Prefix() + suffix
}

// TenantID is for log attrs only; never use it to bypass key prefixing.
func (k Keyspace) TenantID() string {
	return k.tenantID
}

func (k Keyspace) String() string {
	return fmt.Sprintf("Keyspace(%s)", k.Prefix())
}
