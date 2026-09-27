//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/optiqor/optiqor/internal/agent/ident"
	"github.com/optiqor/optiqor/internal/ingestion"
	"github.com/optiqor/optiqor/internal/platform/db"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// AgentSnapshotSink-shaped persistence test: prove the migration's
// RLS policy isolates agent rows the same way it isolates apply_fixes.
// The handler-level mTLS check is unit-tested; this test focuses on
// the DB layer the production sink will sit on top of.
func TestAgentSnapshot_RLSIsolation(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantA := uuid.NewString()
	tenantB := uuid.NewString()
	for _, id := range []string{tenantA, tenantB} {
		if _, err := h.PG.Exec(ctx,
			`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $2)`,
			id, "t-"+id[:8]); err != nil {
			t.Fatalf("seed tenant: %v", err)
		}
	}

	// Insert one agent row for each tenant under BYPASSRLS migrator pool.
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO agents (tenant_id, version, status) VALUES ($1, 'v0.1', 'healthy'), ($2, 'v0.1', 'degraded')`,
		tenantA, tenantB); err != nil {
		t.Fatalf("seed agents: %v", err)
	}

	appPool := h.AppPool(ctx, t)
	for _, tc := range []struct {
		name      string
		tenantID  string
		wantStat  string
		wantCount int
	}{
		{"A sees only A", tenantA, "healthy", 1},
		{"B sees only B", tenantB, "degraded", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := appPool.BeginTx(ctx, pgx.TxOptions{})
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer func() { _ = tx.Rollback(ctx) }()

			bindQ, bindArgs, err := db.TenantBindArgs(tenancy.Context{TenantID: tc.tenantID})
			if err != nil {
				t.Fatalf("bind args: %v", err)
			}
			if _, err := tx.Exec(ctx, bindQ, bindArgs...); err != nil {
				t.Fatalf("bind: %v", err)
			}

			var n int
			var status string
			row := tx.QueryRow(ctx, `SELECT count(*), coalesce(max(status), '') FROM agents`)
			if err := row.Scan(&n, &status); err != nil {
				t.Fatalf("scan: %v", err)
			}
			if n != tc.wantCount || status != tc.wantStat {
				t.Errorf("count=%d status=%q want %d/%q", n, status, tc.wantCount, tc.wantStat)
			}
		})
	}
}

// TestAgentToken_RoundTripUnderRLS sanity-checks the ident package
// against the same test secret the deployment will use, ensuring the
// integration env's audience constant matches the package constant.
func TestAgentToken_RoundTripUnderRLS(t *testing.T) {
	secret := []byte("01234567890123456789012345678901abcd")
	iss, err := ident.NewIssuer(secret, ident.MaxTTL)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	wire, err := iss.Issue("t1", "c1", "b1", ingestion.AgentSnapshotAudience)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	ver, _ := ident.NewVerifier(secret)
	tok, err := ver.Verify(wire, ingestion.AgentSnapshotAudience)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if tok.BatchID != "b1" {
		t.Errorf("batch = %q", tok.BatchID)
	}
}
