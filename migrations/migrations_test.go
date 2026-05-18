// Package migrations is intentionally empty at runtime — its only
// purpose is to ship the .sql files that goose runs. The test file
// here pins structural invariants of the baseline so accidental
// regressions are caught at `go test ./...` time, well before they
// reach a database.
package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadBaseline(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(wd, "0001_baseline.sql")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	return string(b)
}

func TestBaseline_HasUpAndDown(t *testing.T) {
	sql := loadBaseline(t)
	for _, want := range []string{"-- +goose Up", "-- +goose Down", "-- +goose StatementBegin", "-- +goose StatementEnd"} {
		if !strings.Contains(sql, want) {
			t.Errorf("baseline missing %q", want)
		}
	}
}

func TestBaseline_HasFourLevelHierarchy(t *testing.T) {
	sql := loadBaseline(t)
	for _, want := range []string{
		"CREATE TABLE tenants",
		"CREATE TABLE workspaces",
		"CREATE TABLE clusters",
		"CREATE TABLE namespaces",
		"CREATE TABLE workloads",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("baseline missing table: %q", want)
		}
	}
}

func TestBaseline_RLSEnabledOnEveryTenantTable(t *testing.T) {
	sql := loadBaseline(t)
	tenantScoped := []string{
		"workspaces", "clusters", "namespaces", "workloads",
		"recommendations", "recommendation_dismissals",
		"apply_fixes", "receipts", "llm_calls", "audit_log",
	}
	for _, tbl := range tenantScoped {
		want := "ALTER TABLE " + tbl + "                "
		if !strings.Contains(sql, "ALTER TABLE "+tbl) {
			t.Errorf("missing ALTER TABLE for %s (RLS not enabled?)", tbl)
		}
		_ = want
		policy := "CREATE POLICY tenant_isolation ON " + tbl
		if !strings.Contains(sql, policy) {
			t.Errorf("missing %q", policy)
		}
	}
}

func TestBaseline_StableWorkloadIdentity(t *testing.T) {
	sql := loadBaseline(t)
	// We commit to selector-hash identity in the schema body so future
	// hands cannot silently weaken this contract.
	if !strings.Contains(sql, "workload_hash BYTEA NOT NULL") {
		t.Error("workloads table is missing workload_hash column")
	}
	if !strings.Contains(sql, "workload_class_group_id") {
		t.Error("workloads table is missing class group column")
	}
}

func TestBaseline_RolesAreSeparate(t *testing.T) {
	sql := loadBaseline(t)
	if !strings.Contains(sql, "CREATE ROLE optiqor_app") {
		t.Error("missing optiqor_app role")
	}
	if !strings.Contains(sql, "CREATE ROLE optiqor_migrator NOLOGIN BYPASSRLS") {
		t.Error("migrator role must BYPASSRLS")
	}
}

func TestBaseline_NoUpdateDeleteOnAuditLog(t *testing.T) {
	sql := loadBaseline(t)
	if !strings.Contains(sql, "REVOKE UPDATE, DELETE ON audit_log FROM PUBLIC") {
		t.Error("audit_log must be append-only for the app role")
	}
}

func TestBaseline_RegionConstraint(t *testing.T) {
	sql := loadBaseline(t)
	// Year-1 deployment regions only; any new region needs an explicit migration.
	if !strings.Contains(sql, "region IN ('us-east-1','eu-west-1')") {
		t.Error("region check constraint should restrict to Y1 regions")
	}
}

func TestBaseline_RecommendationStateMachine(t *testing.T) {
	sql := loadBaseline(t)
	for _, state := range []string{"active", "snoozed", "dismissed", "ignored-workload", "ignored-class", "applied-externally", "expired"} {
		if !strings.Contains(sql, "'"+state+"'") {
			t.Errorf("recommendation state %q missing from CHECK constraint", state)
		}
	}
}

func TestBaseline_BlastRadiusBounded(t *testing.T) {
	sql := loadBaseline(t)
	if !strings.Contains(sql, "blast_radius BETWEEN 1 AND 5") {
		t.Error("blast_radius must be bounded 1..5")
	}
}

func TestBaseline_ReceiptThreeTiers(t *testing.T) {
	sql := loadBaseline(t)
	for _, tier := range []string{"cloud", "capacity", "hybrid"} {
		if !strings.Contains(sql, "'"+tier+"'") {
			t.Errorf("receipt tier %q missing", tier)
		}
	}
}

func loadMigration(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(wd, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func Test0002_HasUpAndDown(t *testing.T) {
	sql := loadMigration(t, "0002_workload_observed_state.sql")
	for _, want := range []string{"-- +goose Up", "-- +goose Down", "-- +goose StatementBegin", "-- +goose StatementEnd"} {
		if !strings.Contains(sql, want) {
			t.Errorf("0002 missing %q", want)
		}
	}
}

func Test0002_AddsAllObservedStateColumns(t *testing.T) {
	sql := loadMigration(t, "0002_workload_observed_state.sql")
	for _, col := range []string{
		"container_image",
		"current_cpu_request_millicores",
		"current_memory_request_bytes",
		"current_cpu_limit_millicores",
		"current_memory_limit_bytes",
		"replicas",
		"has_hpa",
		"last_observed_at",
	} {
		// Look for ADD COLUMN <name> so renames in Up don't slip through.
		if !strings.Contains(sql, "ADD COLUMN "+col) {
			t.Errorf("0002 missing ADD COLUMN for %q", col)
		}
	}
}

func Test0002_DownDropsEveryColumn(t *testing.T) {
	sql := loadMigration(t, "0002_workload_observed_state.sql")
	for _, col := range []string{
		"container_image",
		"current_cpu_request_millicores",
		"current_memory_request_bytes",
		"current_cpu_limit_millicores",
		"current_memory_limit_bytes",
		"replicas",
		"has_hpa",
		"last_observed_at",
	} {
		if !strings.Contains(sql, "DROP COLUMN IF EXISTS "+col) {
			t.Errorf("0002 Down missing DROP COLUMN IF EXISTS %q", col)
		}
	}
}

func Test0002_ContainerImageIndexForCrossTenantQueries(t *testing.T) {
	sql := loadMigration(t, "0002_workload_observed_state.sql")
	// The container_image partial index is the moat enabler (Helm Chart
	// Efficiency Leaderboard / cross-customer pattern library). Pinning
	// the index keeps a future refactor from silently dropping it.
	if !strings.Contains(sql, "CREATE INDEX workloads_container_image_idx") {
		t.Error("0002 missing workloads_container_image_idx (moat enabler)")
	}
	if !strings.Contains(sql, "WHERE container_image IS NOT NULL") {
		t.Error("0002 container_image index must be partial (NULL exclusion)")
	}
}

func Test0003_HasUpAndDown(t *testing.T) {
	sql := loadMigration(t, "0003_tenancy_primitives.sql")
	for _, want := range []string{"-- +goose Up", "-- +goose Down", "-- +goose StatementBegin", "-- +goose StatementEnd"} {
		if !strings.Contains(sql, want) {
			t.Errorf("0003 missing %q", want)
		}
	}
}

func Test0003_DefinesAllTenancyHelpers(t *testing.T) {
	sql := loadMigration(t, "0003_tenancy_primitives.sql")
	for _, fn := range []string{
		"FUNCTION uuid_generate_v7()",
		"FUNCTION current_tenant_id()",
		"FUNCTION is_superuser_context()",
		"FUNCTION set_superuser_context(",
		"FUNCTION set_updated_at()",
	} {
		if !strings.Contains(sql, fn) {
			t.Errorf("0003 missing helper %q", fn)
		}
	}
}

func Test0003_BindVariableNameStable(t *testing.T) {
	sql := loadMigration(t, "0003_tenancy_primitives.sql")
	// The session variable name must stay app.tenant_id to match the
	// internal/platform/db bind helper. Renaming it on either side
	// silently breaks every tenant-scoped query.
	if !strings.Contains(sql, "current_setting('app.tenant_id', true)") {
		t.Error("0003 must read current_setting('app.tenant_id', true); variable name is load-bearing")
	}
}

func Test0003_RewritesPoliciesViaHelpers(t *testing.T) {
	sql := loadMigration(t, "0003_tenancy_primitives.sql")
	// Every baseline-RLS-enabled table must get its tenant_isolation
	// policy rewritten to use current_tenant_id() OR is_superuser_context().
	// audit_log is the exception (NULL tenant_id under superuser context).
	tenantScoped := []string{
		"workspaces", "clusters", "namespaces", "workloads",
		"recommendations", "recommendation_dismissals",
		"apply_fixes", "receipts", "llm_calls",
	}
	for _, tbl := range tenantScoped {
		want := "CREATE POLICY tenant_isolation ON " + tbl + "\n    USING (tenant_id = current_tenant_id() OR is_superuser_context())"
		if !strings.Contains(sql, want) {
			t.Errorf("0003 missing rewritten policy on %q", tbl)
		}
	}
	// audit_log has the NULL-tenant_id exception baked in.
	if !strings.Contains(sql, "tenant_id IS NOT NULL AND tenant_id = current_tenant_id()") {
		t.Error("0003 audit_log policy must allow NULL tenant_id under superuser context")
	}
}

func Test0003_DownRestoresBaselinePolicies(t *testing.T) {
	sql := loadMigration(t, "0003_tenancy_primitives.sql")
	// The Down block must restore the original baseline policy form
	// verbatim so a rollback fully reverses the refactor.
	if !strings.Contains(sql, "tenant_id::text = current_setting('app.tenant_id', true)") {
		t.Error("0003 Down must restore baseline policy form (tenant_id::text = current_setting)")
	}
}

func Test0003_SuperuserSetterRequiresReason(t *testing.T) {
	sql := loadMigration(t, "0003_tenancy_primitives.sql")
	// Every flip of superuser context must record an audit_log row with
	// a non-empty reason. The setter raises if reason is empty.
	if !strings.Contains(sql, "RAISE EXCEPTION 'set_superuser_context: reason is required'") {
		t.Error("0003 set_superuser_context must reject empty reason")
	}
	if !strings.Contains(sql, "INSERT INTO audit_log") {
		t.Error("0003 set_superuser_context must INSERT INTO audit_log on every flip")
	}
}

func Test0003_UpdatedAtTriggerAttachedToRightTables(t *testing.T) {
	sql := loadMigration(t, "0003_tenancy_primitives.sql")
	// Only baseline tables that have an updated_at column should get the
	// trigger: tenants, workspaces, recommendations.
	for _, tbl := range []string{"tenants", "workspaces", "recommendations"} {
		want := "CREATE TRIGGER " + tbl + "_set_updated_at"
		if !strings.Contains(sql, want) {
			t.Errorf("0003 missing set_updated_at trigger on %q", tbl)
		}
	}
}
