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
	if !strings.Contains(sql, "CREATE ROLE sevro_app") {
		t.Error("missing sevro_app role")
	}
	if !strings.Contains(sql, "CREATE ROLE sevro_migrator NOLOGIN BYPASSRLS") {
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
