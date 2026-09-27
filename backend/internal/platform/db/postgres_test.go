package db

import (
	"errors"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestTenantBindArgs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		in       tenancy.Context
		wantErr  error
		wantSQL  string
		wantArgs []string
	}{
		{
			name:    "empty tenant rejected",
			in:      tenancy.Context{},
			wantErr: tenancy.ErrNoTenant,
		},
		{
			name:     "tenant only",
			in:       tenancy.Context{TenantID: "t1"},
			wantSQL:  TenantStmtSQL,
			wantArgs: []string{"t1"},
		},
		{
			name:     "tenant and workspace",
			in:       tenancy.Context{TenantID: "t1", WorkspaceID: "w1"},
			wantSQL:  TenantWorkspaceStmtSQL,
			wantArgs: []string{"t1", "w1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, err := TenantBindArgs(tc.in)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if sql != tc.wantSQL {
				t.Errorf("sql = %q, want %q", sql, tc.wantSQL)
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tc.wantArgs)
			}
			for i, want := range tc.wantArgs {
				if args[i] != want {
					t.Errorf("args[%d] = %v, want %q", i, args[i], want)
				}
			}
		})
	}
}

// TenantStmtSQL must bind via parameter (not interpolate) — guard
// against regressions that introduce SQL injection.
func TestTenantStmtSQL_UsesParameter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sql     string
		wantSub string
	}{
		{"tenant stmt has $1", TenantStmtSQL, "$1"},
		{"tenant+ws stmt has $1", TenantWorkspaceStmtSQL, "$1"},
		{"tenant+ws stmt has $2", TenantWorkspaceStmtSQL, "$2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.sql, tc.wantSub) {
				t.Errorf("statement must contain %q: %q", tc.wantSub, tc.sql)
			}
		})
	}
}
