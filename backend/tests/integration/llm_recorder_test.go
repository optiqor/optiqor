//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/platform/db"
	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestPgRecorder_RecordsUnderRLS(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	if _, err := h.PG.Exec(ctx,
		`INSERT INTO tenants (id, slug, name) VALUES ($1, $2, 'A'), ($3, $4, 'B')`,
		tenantA, "tenant-a-"+tenantA[:8], tenantB, "tenant-b-"+tenantB[:8]); err != nil {
		t.Fatalf("seed tenants: %v", err)
	}

	appPool := h.AppPool(ctx, t)
	rec := agent.NewPgRecorder(appPool)
	rec.Now = func() time.Time { return time.Date(2026, 5, 28, 9, 0, 0, 0, time.UTC) }

	inHash := sha256.Sum256([]byte("system+user"))
	outHash := sha256.Sum256([]byte("EXPLANATION:\n...DIFF:\n..."))

	if err := rec.Record(ctx, tenancy.Context{TenantID: tenantA}, agent.CallRecord{
		Workload:       "api",
		Purpose:        "apply-fix",
		Model:          "claude-sonnet-4-6",
		InputTokens:    1200,
		OutputTokens:   320,
		CacheHitTokens: 900,
		CostUSDCents:   18,
		DurationMS:     742,
		InputHash:      inHash,
		OutputHash:     outHash,
	}); err != nil {
		t.Fatalf("Record A: %v", err)
	}
	if err := rec.Record(ctx, tenancy.Context{TenantID: tenantB}, agent.CallRecord{
		Model:        "claude-haiku-4-5",
		InputTokens:  200,
		OutputTokens: 50,
		CostUSDCents: 2,
	}); err != nil {
		t.Fatalf("Record B: %v", err)
	}

	// Migrator pool (BYPASSRLS) sees both rows; verify per-tenant
	// projection through the RLS app pool.
	for _, tc := range []struct {
		name      string
		tenantID  string
		wantCount int
		wantModel string
	}{
		{name: "A sees only A", tenantID: tenantA, wantCount: 1, wantModel: "sonnet"},
		{name: "B sees only B", tenantID: tenantB, wantCount: 1, wantModel: "haiku"},
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

			var (
				model    string
				count    int
				cacheHit int
			)
			row := tx.QueryRow(ctx, `SELECT count(*), coalesce(max(model),''), coalesce(max(cache_hit_tokens),0) FROM llm_calls`)
			if err := row.Scan(&count, &model, &cacheHit); err != nil {
				t.Fatalf("scan: %v", err)
			}
			if count != tc.wantCount {
				t.Fatalf("count = %d, want %d", count, tc.wantCount)
			}
			if model != tc.wantModel {
				t.Errorf("model = %q, want %q", model, tc.wantModel)
			}
			if tc.tenantID == tenantA && cacheHit != 900 {
				t.Errorf("tenant A cache_hit_tokens = %d, want 900", cacheHit)
			}
		})
	}
}

func TestPgRecorder_RejectsInvalidTenant(t *testing.T) {
	h := New(t)
	ctx := context.Background()
	rec := agent.NewPgRecorder(h.PG)
	if err := rec.Record(ctx, tenancy.Context{}, agent.CallRecord{Model: "claude-sonnet"}); err == nil {
		t.Fatal("want validation error on empty tenant")
	}
}
