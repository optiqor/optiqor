//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/optiqor/optiqor/internal/applyfix/attribution"
	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestAttribution_PgStore_LookupAndMarkMerged(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantID := uuid.NewString()
	wsID := uuid.NewString()
	clusterID := uuid.NewString()
	nsID := uuid.NewString()
	wlID := uuid.NewString()
	recID := uuid.NewString()
	afID := uuid.NewString()

	if _, err := h.PG.Exec(ctx, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, 'A')`, tenantID, "tenant-"+tenantID[:8]); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := h.PG.Exec(ctx, `INSERT INTO workspaces (id, tenant_id, slug, name) VALUES ($1, $2, 'default', 'A-default')`, wsID, tenantID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := h.PG.Exec(ctx, `INSERT INTO clusters (id, tenant_id, workspace_id, name) VALUES ($1, $2, $3, 'prod')`, clusterID, tenantID, wsID); err != nil {
		t.Fatalf("seed cluster: %v", err)
	}
	if _, err := h.PG.Exec(ctx, `INSERT INTO namespaces (id, tenant_id, cluster_id, name) VALUES ($1, $2, $3, 'default')`, nsID, tenantID, clusterID); err != nil {
		t.Fatalf("seed namespace: %v", err)
	}
	if _, err := h.PG.Exec(ctx, `INSERT INTO workloads (id, tenant_id, namespace_id, kind, name) VALUES ($1, $2, $3, 'Deployment', 'api')`, wlID, tenantID, nsID); err != nil {
		t.Fatalf("seed workload: %v", err)
	}
	if _, err := h.PG.Exec(ctx, `INSERT INTO recommendations
		(id, tenant_id, workload_id, detector_id, severity, confidence, title, detail, monthly_usd_cents)
		VALUES ($1, $2, $3, 'cpu-overprovisioned', 'MED', 'high', 'CPU overprovisioned', '', 2920)`,
		recID, tenantID, wlID); err != nil {
		t.Fatalf("seed recommendation: %v", err)
	}
	if _, err := h.PG.Exec(ctx, `INSERT INTO apply_fixes
		(id, tenant_id, recommendation_id, vcs, repo, pr_number, pr_url, state)
		VALUES ($1, $2, $3, 'github', 'acme/api', 42, 'https://github.com/acme/api/pull/42', 'open')`,
		afID, tenantID, recID); err != nil {
		t.Fatalf("seed apply_fix: %v", err)
	}

	appPool := h.AppPool(ctx, t)
	store := attribution.NewPgStore(appPool)

	row, err := store.LookupByRepoPR(ctx, tenancy.Context{TenantID: tenantID}, "acme/api", 42)
	if err != nil {
		t.Fatalf("LookupByRepoPR: %v", err)
	}
	if row.ID != afID {
		t.Errorf("row.ID = %q, want %q", row.ID, afID)
	}
	if row.TenantID != tenantID {
		t.Errorf("row.TenantID = %q, want %q", row.TenantID, tenantID)
	}
	if row.MonthlySavingsUSDCents != 2920 {
		t.Errorf("monthly = %d, want 2920", row.MonthlySavingsUSDCents)
	}
	if row.Title != "CPU overprovisioned" {
		t.Errorf("title = %q", row.Title)
	}

	mergedAt := time.Date(2026, 5, 27, 23, 12, 0, 0, time.UTC)
	if err := store.MarkMerged(ctx, tenancy.Context{TenantID: tenantID}, afID, mergedAt); err != nil {
		t.Fatalf("MarkMerged: %v", err)
	}

	// Subsequent lookup must miss — the lookup filters `state <> 'merged'`.
	if _, err := store.LookupByRepoPR(ctx, tenancy.Context{TenantID: tenantID}, "acme/api", 42); !errors.Is(err, attribution.ErrNoMatchingApplyFix) {
		t.Errorf("post-merge lookup: err = %v, want ErrNoMatchingApplyFix", err)
	}

	// And the row in DB should be state=merged.
	var state string
	var got time.Time
	if err := h.PG.QueryRow(ctx, `SELECT state, merged_at FROM apply_fixes WHERE id = $1`, afID).Scan(&state, &got); err != nil {
		t.Fatal(err)
	}
	if state != "merged" {
		t.Errorf("state = %q, want merged", state)
	}
	if !got.Equal(mergedAt) {
		t.Errorf("merged_at = %v, want %v", got, mergedAt)
	}
}

func TestAttribution_PgResolver_BypassesRLSWithAudit(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, 'A'), ($3, $4, 'B')`,
		tenantA, "tenant-a-"+tenantA[:8], tenantB, "tenant-b-"+tenantB[:8]); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO vcs_installations (tenant_id, provider, installation_id, account_login) VALUES ($1, 'github', 12345, 'acme')`,
		tenantA); err != nil {
		t.Fatalf("seed installation: %v", err)
	}

	appPool := h.AppPool(ctx, t)
	r := attribution.NewPgResolver(appPool)

	got, err := r.ResolveInstallation(ctx, "github", 12345)
	if err != nil {
		t.Fatalf("ResolveInstallation: %v", err)
	}
	if got != tenantA {
		t.Errorf("tenant = %q, want %q", got, tenantA)
	}

	// Audit row recorded — verify under BYPASSRLS.
	var n int
	if err := h.PG.QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE action IN ('superuser_context.enable','superuser_context.disable')
		   AND metadata->>'reason' LIKE 'vcs webhook resolve installation github/12345'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Errorf("audit_log enable+disable rows = %d, want >= 2", n)
	}

	if _, err := r.ResolveInstallation(ctx, "github", 999); !errors.Is(err, attribution.ErrUnknownInstallation) {
		t.Errorf("unknown id: err = %v, want ErrUnknownInstallation", err)
	}
}
