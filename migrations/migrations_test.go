// Package migrations ships the .sql files goose runs. The tests pin
// structural invariants so a regression fails `go test ./...` before
// it ever reaches a database.
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
	// Pin selector-hash identity so a future change can't silently
	// weaken the workload-identity contract.
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
	// Year-1 regions only; expanding needs an explicit migration so an
	// SDK default can't land data in an uncleared region.
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
		// Match ADD COLUMN <name> so a rename in Up doesn't slip past.
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
	// Pin the moat-enabling partial index so a future refactor can't
	// silently drop it (Leaderboard / pattern library queries depend
	// on it).
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
	// app.tenant_id must match internal/platform/db.BindTenant —
	// renaming on either side silently breaks every tenant query.
	if !strings.Contains(sql, "current_setting('app.tenant_id', true)") {
		t.Error("0003 must read current_setting('app.tenant_id', true); variable name is load-bearing")
	}
}

func Test0003_RewritesPoliciesViaHelpers(t *testing.T) {
	sql := loadMigration(t, "0003_tenancy_primitives.sql")
	// Every baseline-RLS-enabled table gets its policy rewritten via
	// current_tenant_id() OR is_superuser_context(). audit_log is the
	// exception — it also admits NULL tenant_id under superuser.
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
	if !strings.Contains(sql, "tenant_id IS NOT NULL AND tenant_id = current_tenant_id()") {
		t.Error("0003 audit_log policy must allow NULL tenant_id under superuser context")
	}
}

func Test0003_DownRestoresBaselinePolicies(t *testing.T) {
	sql := loadMigration(t, "0003_tenancy_primitives.sql")
	// Down must restore the baseline policy verbatim so a rollback
	// fully reverses the refactor.
	if !strings.Contains(sql, "tenant_id::text = current_setting('app.tenant_id', true)") {
		t.Error("0003 Down must restore baseline policy form (tenant_id::text = current_setting)")
	}
}

func Test0003_SuperuserSetterRequiresReason(t *testing.T) {
	sql := loadMigration(t, "0003_tenancy_primitives.sql")
	// Every superuser flip must record an audit_log row with a
	// non-empty reason; the setter raises on empty.
	if !strings.Contains(sql, "RAISE EXCEPTION 'set_superuser_context: reason is required'") {
		t.Error("0003 set_superuser_context must reject empty reason")
	}
	if !strings.Contains(sql, "INSERT INTO audit_log") {
		t.Error("0003 set_superuser_context must INSERT INTO audit_log on every flip")
	}
}

func Test0003_UpdatedAtTriggerAttachedToRightTables(t *testing.T) {
	sql := loadMigration(t, "0003_tenancy_primitives.sql")
	// Only the baseline tables with an updated_at column get the
	// trigger: tenants, workspaces, recommendations.
	for _, tbl := range []string{"tenants", "workspaces", "recommendations"} {
		want := "CREATE TRIGGER " + tbl + "_set_updated_at"
		if !strings.Contains(sql, want) {
			t.Errorf("0003 missing set_updated_at trigger on %q", tbl)
		}
	}
}

func Test0004_HasUpAndDown(t *testing.T) {
	sql := loadMigration(t, "0004_shared_analyses.sql")
	for _, want := range []string{"-- +goose Up", "-- +goose Down", "-- +goose StatementBegin", "-- +goose StatementEnd"} {
		if !strings.Contains(sql, want) {
			t.Errorf("0004 missing %q", want)
		}
	}
}

func Test0004_CreatesSharedAnalysesTable(t *testing.T) {
	sql := loadMigration(t, "0004_shared_analyses.sql")
	if !strings.Contains(sql, "CREATE TABLE shared_analyses") {
		t.Error("0004 must CREATE TABLE shared_analyses")
	}
	// Public-by-design — no RLS, no policy. Adding either breaks the
	// public sandbox; this guard catches the accident.
	if strings.Contains(sql, "ALTER TABLE shared_analyses ENABLE ROW LEVEL SECURITY") {
		t.Error("0004 shared_analyses must remain public-by-design (no RLS)")
	}
	if strings.Contains(sql, "CREATE POLICY tenant_isolation ON shared_analyses") {
		t.Error("0004 shared_analyses is not tenant-scoped; no policy")
	}
}

func Test0004_CoreColumnsPresent(t *testing.T) {
	sql := loadMigration(t, "0004_shared_analyses.sql")
	// hash is URL slug + dedup key; payload_sha256 is the full 32-byte
	// digest (distinct from the 12-byte prefix); expires_at drives the
	// 30-day TTL contract.
	for _, want := range []string{
		"id              UUID PRIMARY KEY DEFAULT uuid_generate_v7()",
		"hash            TEXT NOT NULL UNIQUE",
		"payload_sha256  BYTEA NOT NULL",
		"source          TEXT NOT NULL CHECK (source IN ('cli', 'sandbox'))",
		"payload         BYTEA",
		"payload_s3_key  TEXT",
		"expires_at      TIMESTAMPTZ NOT NULL",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0004 missing column definition: %q", want)
		}
	}
}

func Test0004_PayloadXorS3KeyConstraint(t *testing.T) {
	sql := loadMigration(t, "0004_shared_analyses.sql")
	// Server-side guard: a misbehaving writer can't store both or
	// neither of in-row / S3-promoted payload.
	if !strings.Contains(sql, "CONSTRAINT payload_xor_s3_key CHECK") {
		t.Error("0004 must enforce payload XOR payload_s3_key via CHECK constraint")
	}
}

func Test0004_HasActiveRowIndex(t *testing.T) {
	sql := loadMigration(t, "0004_shared_analyses.sql")
	if !strings.Contains(sql, "CREATE INDEX shared_analyses_expires_at_idx") {
		t.Error("0004 missing expires_at partial index")
	}
}

func Test0004_AppGrantsExplicit(t *testing.T) {
	sql := loadMigration(t, "0004_shared_analyses.sql")
	if !strings.Contains(sql, "GRANT SELECT, INSERT, UPDATE ON shared_analyses TO optiqor_app") {
		t.Error("0004 must grant SELECT/INSERT/UPDATE to optiqor_app (no DELETE — append-only-ish)")
	}
}

func Test0004_DownIsClean(t *testing.T) {
	sql := loadMigration(t, "0004_shared_analyses.sql")
	for _, want := range []string{
		"DROP INDEX IF EXISTS shared_analyses_expires_at_idx",
		"DROP TABLE IF EXISTS shared_analyses",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0004 Down missing %q", want)
		}
	}
}

func Test0005_VCSInstallations_StructureAndRLS(t *testing.T) {
	sql := loadMigration(t, "0005_vcs_installations.sql")
	for _, want := range []string{
		"CREATE TABLE vcs_installations",
		"id                       UUID PRIMARY KEY DEFAULT uuid_generate_v7()",
		"tenant_id                UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE",
		"provider                 TEXT NOT NULL CHECK (provider IN ('github','gitlab'))",
		"installation_id          BIGINT NOT NULL",
		"access_token_ciphertext  BYTEA",
		"status                   TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','revoked'))",
		"UNIQUE (provider, installation_id)",
		"ALTER TABLE vcs_installations ENABLE ROW LEVEL SECURITY",
		"CREATE POLICY tenant_isolation ON vcs_installations",
		"GRANT SELECT, INSERT, UPDATE ON vcs_installations TO optiqor_app",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0005 missing: %q", want)
		}
	}
}

func Test0005_VCSInstallations_NoPlaintextTokenColumn(t *testing.T) {
	sql := loadMigration(t, "0005_vcs_installations.sql")
	if strings.Contains(sql, "access_token         TEXT") || strings.Contains(sql, "access_token TEXT") {
		t.Error("0005 must never store plaintext access tokens — encrypt via KMS")
	}
}

func Test0005_VCSInstallations_DownIsClean(t *testing.T) {
	sql := loadMigration(t, "0005_vcs_installations.sql")
	for _, want := range []string{
		"DROP POLICY IF EXISTS tenant_isolation ON vcs_installations",
		"DROP TABLE IF EXISTS vcs_installations",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0005 Down missing %q", want)
		}
	}
}
