//go:build integration

package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/optiqor/optiqor/internal/dashboard"
	"github.com/optiqor/optiqor/internal/ingestion"
	"github.com/optiqor/optiqor/internal/platform/db"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/worker/workflows"
)

func TestAgentSnapshotPgSink_PersistAndProjectsHealth(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantID := uuid.NewString()
	wsID := uuid.NewString()
	clusterID := uuid.NewString()
	if _, err := h.PG.Exec(ctx, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, 'A')`, tenantID, "tenant-"+tenantID[:8]); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := h.PG.Exec(ctx, `INSERT INTO workspaces (id, tenant_id, slug, name) VALUES ($1, $2, 'default', 'A-default')`, wsID, tenantID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := h.PG.Exec(ctx, `INSERT INTO clusters (id, tenant_id, workspace_id, name) VALUES ($1, $2, $3, 'prod')`, clusterID, tenantID, wsID); err != nil {
		t.Fatalf("seed cluster: %v", err)
	}

	appPool := h.AppPool(ctx, t)
	sink := ingestion.NewAgentSnapshotPgSink(appPool)
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	sink.Now = func() time.Time { return now }

	snap := ingestion.AgentSnapshot{
		BatchID:          "b1",
		CapturedAt:       now,
		ClusterID:        clusterID,
		AgentVersion:     "v0.5.0",
		ProvisionerClass: "karpenter",
		Health:           &ingestion.AgentHealthSnap{Version: "v0.5.0", DataFreshnessSeconds: 30},
	}
	if err := sink.Persist(tenancy.Context{TenantID: tenantID}, snap); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	store := dashboard.NewAgentHealthPgStore(appPool)
	store.Now = func() time.Time { return now.Add(45 * time.Second) }
	got, err := store.Latest(tenancy.Context{TenantID: tenantID})
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if got.Status != "healthy" {
		t.Errorf("status = %q, want healthy", got.Status)
	}
	if got.Version != "v0.5.0" {
		t.Errorf("version = %q, want v0.5.0", got.Version)
	}
	// Wall-clock since last_seen_at is 45s, larger than the snap-reported
	// 30s, so the projection should override with the bigger gap.
	if got.DataFreshnessSeconds != 45 {
		t.Errorf("freshness = %d, want 45", got.DataFreshnessSeconds)
	}

	// Snap with a stale freshness (> 6h) should land as offline.
	snap.Health.DataFreshnessSeconds = 7 * 3600
	if err := sink.Persist(tenancy.Context{TenantID: tenantID}, snap); err != nil {
		t.Fatalf("Persist 2: %v", err)
	}
	got2, err := store.Latest(tenancy.Context{TenantID: tenantID})
	if err != nil {
		t.Fatalf("Latest 2: %v", err)
	}
	if got2.Status != "offline" {
		t.Errorf("status (stale) = %q, want offline", got2.Status)
	}

	// Provisioner class projected onto clusters row.
	tx, err := appPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	bindQ, bindArgs, err := db.TenantBindArgs(tenancy.Context{TenantID: tenantID})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if _, err := tx.Exec(ctx, bindQ, bindArgs...); err != nil {
		t.Fatalf("bind exec: %v", err)
	}
	var cls string
	if err := tx.QueryRow(ctx, `SELECT node_provisioner_class FROM clusters WHERE id::text = $1`, clusterID).Scan(&cls); err != nil {
		t.Fatalf("scan cluster class: %v", err)
	}
	if cls != "karpenter" {
		t.Errorf("node_provisioner_class = %q, want karpenter", cls)
	}
}

// Concurrent persists must collapse into a single row via the partial
// unique index from migration 0010. Pre-0010 the refresh-then-insert
// pattern raced and produced duplicates; this test pins the contract.
func TestAgentSnapshotPgSink_ConcurrentPersistCollapses(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantID := uuid.NewString()
	wsID := uuid.NewString()
	clusterID := uuid.NewString()
	if _, err := h.PG.Exec(ctx, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, 'A')`, tenantID, "tenant-"+tenantID[:8]); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := h.PG.Exec(ctx, `INSERT INTO workspaces (id, tenant_id, slug, name) VALUES ($1, $2, 'default', 'A-default')`, wsID, tenantID); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := h.PG.Exec(ctx, `INSERT INTO clusters (id, tenant_id, workspace_id, name) VALUES ($1, $2, $3, 'prod')`, clusterID, tenantID, wsID); err != nil {
		t.Fatalf("seed cluster: %v", err)
	}

	appPool := h.AppPool(ctx, t)
	sink := ingestion.NewAgentSnapshotPgSink(appPool)

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	start := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			snap := ingestion.AgentSnapshot{
				BatchID:      "b" + uuid.NewString()[:8],
				CapturedAt:   start.Add(time.Duration(i) * time.Second),
				ClusterID:    clusterID,
				AgentVersion: "v0.5.0",
				Health:       &ingestion.AgentHealthSnap{DataFreshnessSeconds: 30},
			}
			errs <- sink.Persist(tenancy.Context{TenantID: tenantID}, snap)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Persist concurrent: %v", err)
		}
	}

	var n int
	if err := h.PG.QueryRow(ctx,
		`SELECT count(*) FROM agents WHERE tenant_id = $1 AND cluster_id = $2`,
		tenantID, clusterID,
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("agents row count = %d after %d concurrent persists; want 1", n, workers)
	}
}

// Two-tenant isolation: a snapshot from tenant A must not surface to
// tenant B's reader, even though both share the same Pool. The reader
// runs through RLS, so a missing or mistyped policy would leak A's row
// into B's pill. This wires the production sink + reader, not raw SQL.
func TestAgentSnapshotPgSink_RLSIsolationAcrossTenants(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	type tnt struct {
		id, wsID, clusterID string
	}
	tenants := []tnt{
		{uuid.NewString(), uuid.NewString(), uuid.NewString()},
		{uuid.NewString(), uuid.NewString(), uuid.NewString()},
	}
	for i, tn := range tenants {
		if _, err := h.PG.Exec(ctx, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`, tn.id, "t-"+tn.id[:8], "tenant-"+tn.id[:4]); err != nil {
			t.Fatalf("seed tenant %d: %v", i, err)
		}
		if _, err := h.PG.Exec(ctx, `INSERT INTO workspaces (id, tenant_id, slug, name) VALUES ($1, $2, 'default', $3)`, tn.wsID, tn.id, "ws-"+tn.id[:4]); err != nil {
			t.Fatalf("seed workspace %d: %v", i, err)
		}
		if _, err := h.PG.Exec(ctx, `INSERT INTO clusters (id, tenant_id, workspace_id, name) VALUES ($1, $2, $3, $4)`, tn.clusterID, tn.id, tn.wsID, "cluster-"+tn.id[:4]); err != nil {
			t.Fatalf("seed cluster %d: %v", i, err)
		}
	}

	appPool := h.AppPool(ctx, t)
	sink := ingestion.NewAgentSnapshotPgSink(appPool)
	versions := []string{"v0.5.0-A", "v0.5.0-B"}
	for i, tn := range tenants {
		if err := sink.Persist(tenancy.Context{TenantID: tn.id}, ingestion.AgentSnapshot{
			CapturedAt:   time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
			ClusterID:    tn.clusterID,
			AgentVersion: versions[i],
			Health:       &ingestion.AgentHealthSnap{DataFreshnessSeconds: 30},
		}); err != nil {
			t.Fatalf("Persist %d: %v", i, err)
		}
	}

	store := dashboard.NewAgentHealthPgStore(appPool)
	for i, tn := range tenants {
		got, err := store.Latest(tenancy.Context{TenantID: tn.id})
		if err != nil {
			t.Fatalf("Latest %d: %v", i, err)
		}
		if got.Version != versions[i] {
			t.Errorf("tenant %d saw version %q, want %q (cross-tenant leak)", i, got.Version, versions[i])
		}
	}
}

func TestTenantSettingsPgSource_ReadsSkepticDefault(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantID := uuid.NewString()
	if _, err := h.PG.Exec(ctx, `INSERT INTO tenants (id, slug, name, skeptic_mode_default) VALUES ($1, $2, 'A', true)`,
		tenantID, "tenant-"+tenantID[:8]); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	src := workflows.NewTenantSettingsPgSource(h.AppPool(ctx, t))
	v, err := src.SkepticModeDefault(ctx, tenancy.Context{TenantID: tenantID})
	if err != nil {
		t.Fatalf("SkepticModeDefault: %v", err)
	}
	if !v {
		t.Errorf("want true, got %v", v)
	}

	// Flip the column and re-read; ensures the source reads live not cached.
	if _, err := h.PG.Exec(ctx, `UPDATE tenants SET skeptic_mode_default = false WHERE id = $1`, tenantID); err != nil {
		t.Fatalf("update tenant: %v", err)
	}
	v2, err := src.SkepticModeDefault(ctx, tenancy.Context{TenantID: tenantID})
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if v2 {
		t.Errorf("want false after flip, got %v", v2)
	}
}
