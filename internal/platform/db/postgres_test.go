package db

import (
	"errors"
	"testing"

	"github.com/lowplane/backend/internal/tenancy"
)

func TestTenantBindArgs_RequiresTenant(t *testing.T) {
	_, _, err := TenantBindArgs(tenancy.Context{})
	if !errors.Is(err, tenancy.ErrNoTenant) {
		t.Fatalf("expected ErrNoTenant, got %v", err)
	}
}

func TestTenantBindArgs_TenantOnly(t *testing.T) {
	sql, args, err := TenantBindArgs(tenancy.Context{TenantID: "t1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sql != TenantStmtSQL {
		t.Errorf("sql = %q, want %q", sql, TenantStmtSQL)
	}
	if len(args) != 1 || args[0] != "t1" {
		t.Errorf("args = %v, want [t1]", args)
	}
}

func TestTenantBindArgs_WithWorkspace(t *testing.T) {
	sql, args, err := TenantBindArgs(tenancy.Context{TenantID: "t1", WorkspaceID: "w1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sql != TenantWorkspaceStmtSQL {
		t.Errorf("sql = %q, want %q", sql, TenantWorkspaceStmtSQL)
	}
	if len(args) != 2 || args[0] != "t1" || args[1] != "w1" {
		t.Errorf("args = %v, want [t1 w1]", args)
	}
}

// TenantStmtSQL must use a parameter (not interpolate) — guard against
// regressions that introduce SQL injection.
func TestTenantStmtSQL_UsesParameter(t *testing.T) {
	for _, s := range []string{TenantStmtSQL, TenantWorkspaceStmtSQL} {
		if !contains(s, "$1") {
			t.Errorf("statement must bind via $1: %q", s)
		}
	}
	if contains(TenantWorkspaceStmtSQL, "$2") == false {
		t.Errorf("two-arg statement must use $2: %q", TenantWorkspaceStmtSQL)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
