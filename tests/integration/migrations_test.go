//go:build integration

package integration

import (
	"context"
	"testing"
)

func TestMigrations_ApplyAndSchema(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		query string
		want  bool
	}{
		{"tenants table", "SELECT to_regclass('public.tenants') IS NOT NULL", true},
		{"workspaces table", "SELECT to_regclass('public.workspaces') IS NOT NULL", true},
		{"clusters table", "SELECT to_regclass('public.clusters') IS NOT NULL", true},
		{"workloads table", "SELECT to_regclass('public.workloads') IS NOT NULL", true},
		{"receipts table", "SELECT to_regclass('public.receipts') IS NOT NULL", true},
		{"apply_fixes table", "SELECT to_regclass('public.apply_fixes') IS NOT NULL", true},
		{"audit_log table", "SELECT to_regclass('public.audit_log') IS NOT NULL", true},
		{"shared_analyses table", "SELECT to_regclass('public.shared_analyses') IS NOT NULL", true},
		{"vcs_installations table", "SELECT to_regclass('public.vcs_installations') IS NOT NULL", true},
		{"optiqor_app role exists", "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='optiqor_app')", true},
		{"optiqor_migrator role exists", "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='optiqor_migrator')", true},
		{"optiqor_migrator BYPASSRLS", "SELECT rolbypassrls FROM pg_roles WHERE rolname='optiqor_migrator'", true},
		{"optiqor_app NOT BYPASSRLS", "SELECT rolbypassrls FROM pg_roles WHERE rolname='optiqor_app'", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got bool
			if err := h.PG.QueryRow(ctx, tc.query).Scan(&got); err != nil {
				t.Fatalf("query: %v", err)
			}
			if got != tc.want {
				t.Errorf("%s = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestMigrations_RLSEnabledOnTenantTables(t *testing.T) {
	h := New(t)
	ctx := context.Background()

	// shared_analyses is intentionally public (migration 0004) — left out.
	tenantScoped := []string{
		"workspaces", "clusters", "namespaces", "workloads",
		"recommendations", "recommendation_dismissals",
		"apply_fixes", "receipts", "llm_calls", "audit_log",
		"vcs_installations",
	}
	for _, table := range tenantScoped {
		t.Run(table, func(t *testing.T) {
			var enabled bool
			err := h.PG.QueryRow(ctx,
				`SELECT relrowsecurity FROM pg_class WHERE relname = $1`, table).
				Scan(&enabled)
			if err != nil {
				t.Fatalf("%s: %v", table, err)
			}
			if !enabled {
				t.Errorf("table %s has RLS disabled — tenant data will leak", table)
			}
		})
	}
}

// shared_analyses is public-by-design per migration 0004; the hash is
// the access token. Flipping RLS on would break /r/<hash> reads.
func TestMigrations_SharedAnalysesIsPublic(t *testing.T) {
	h := New(t)
	var enabled bool
	err := h.PG.QueryRow(context.Background(),
		`SELECT relrowsecurity FROM pg_class WHERE relname = 'shared_analyses'`).
		Scan(&enabled)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Error("shared_analyses has RLS enabled — public share reads will fail")
	}
}
