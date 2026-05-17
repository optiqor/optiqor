package db

import "github.com/optiqor/optiqor/internal/tenancy"

// TenantStmtSQL is the parameterised statement domain code runs at the
// start of every tenant-scoped transaction. It binds the tenant id to a
// session-local GUC; RLS policies on every tenant-scoped table read
// `current_setting('app.tenant_id', true)` to enforce isolation
// server-side.
//
// The query takes one argument: the tenant id. The driver binds it
// safely; never interpolate the value into SQL.
const TenantStmtSQL = `SELECT set_config('app.tenant_id', $1, true)`

// TenantWorkspaceStmtSQL additionally binds the workspace id for finer
// scoping (enterprise customers whose security teams see only some
// workspaces). The query takes two args: tenant id, workspace id.
const TenantWorkspaceStmtSQL = `SELECT set_config('app.tenant_id', $1, true), set_config('app.workspace_id', $2, true)`

// TenantBindArgs returns the (sql, args) pair to run on a fresh
// connection. Callers should run this inside the same transaction as
// any subsequent tenant-scoped query so SET LOCAL semantics apply.
//
// If the tenancy.Context has a workspace id set, the workspace GUC is
// bound too.
func TenantBindArgs(t tenancy.Context) (query string, args []any, err error) {
	if err := t.Validate(); err != nil {
		return "", nil, err
	}
	if t.WorkspaceID != "" {
		return TenantWorkspaceStmtSQL, []any{t.TenantID, t.WorkspaceID}, nil
	}
	return TenantStmtSQL, []any{t.TenantID}, nil
}
