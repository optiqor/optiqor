//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/optiqor/optiqor/internal/platform/db"
	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestRLS_CrossTenantIsolation(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	wsA, wsB := uuid.NewString(), uuid.NewString()

	if _, err := h.PG.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, 'A'), ($3, $4, 'B')`,
		tenantA, "tenant-a-"+tenantA[:8], tenantB, "tenant-b-"+tenantB[:8]); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO workspaces (id, tenant_id, slug, name) VALUES ($1, $2, 'default', 'A-default'), ($3, $4, 'default', 'B-default')`,
		wsA, tenantA, wsB, tenantB); err != nil {
		t.Fatalf("seed workspaces: %v", err)
	}

	appPool := h.AppPool(ctx, t)

	for _, tc := range []struct {
		name      string
		bind      tenancy.Context
		wantCount int
		wantSeen  string
	}{
		{name: "tenant A sees only A", bind: tenancy.Context{TenantID: tenantA}, wantCount: 1, wantSeen: wsA},
		{name: "tenant B sees only B", bind: tenancy.Context{TenantID: tenantB}, wantCount: 1, wantSeen: wsB},
		{name: "empty tenant sees nothing", bind: tenancy.Context{TenantID: "00000000-0000-0000-0000-000000000000"}, wantCount: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := appPool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer func() { _ = tx.Rollback(ctx) }()

			bindQ, bindArgs, err := db.TenantBindArgs(tc.bind)
			if err != nil {
				t.Fatalf("bind args: %v", err)
			}
			if _, err := tx.Exec(ctx, bindQ, bindArgs...); err != nil {
				t.Fatalf("bind tenant: %v", err)
			}

			rows, err := tx.Query(ctx, `SELECT id::text FROM workspaces`)
			if err != nil {
				t.Fatalf("select: %v", err)
			}
			defer rows.Close()
			var ids []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					t.Fatalf("scan: %v", err)
				}
				ids = append(ids, id)
			}
			if len(ids) != tc.wantCount {
				t.Fatalf("workspace count = %d (%v), want %d", len(ids), ids, tc.wantCount)
			}
			if tc.wantSeen != "" && ids[0] != tc.wantSeen {
				t.Errorf("saw %q, want %q", ids[0], tc.wantSeen)
			}
		})
	}
}

// Pins the fail-closed shape: app-role query with no tenant bound
// returns 0 rows, never leaks. SET LOCAL only spans its wrapping TX.
func TestRLS_OutsideTransactionLeaks(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantA := uuid.NewString()
	wsA := uuid.NewString()
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, 'A')`, tenantA, "leak-test-"+tenantA[:8]); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO workspaces (id, tenant_id, slug, name) VALUES ($1, $2, 'default', 'leak')`, wsA, tenantA); err != nil {
		t.Fatalf("seed ws: %v", err)
	}

	appPool := h.AppPool(ctx, t)
	var n int
	if err := appPool.QueryRow(ctx, `SELECT count(*) FROM workspaces`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Errorf("unbound app role saw %d workspaces — RLS not fail-closed", n)
	}
}
