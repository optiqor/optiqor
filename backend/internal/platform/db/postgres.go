package db

import "github.com/optiqor/optiqor/internal/tenancy"

// TenantStmtSQL binds the tenant id to the `app.tenant_id` session GUC.
// The name is load-bearing: every RLS policy reads
// current_setting('app.tenant_id', true). Rename one side, break every
// query on the other. Always bound via $1 — never interpolate.
const TenantStmtSQL = `SELECT set_config('app.tenant_id', $1, true)`

// TenantWorkspaceStmtSQL also binds app.workspace_id for enterprise
// scoping. Args: tenant id, workspace id.
const TenantWorkspaceStmtSQL = `SELECT set_config('app.tenant_id', $1, true), set_config('app.workspace_id', $2, true)`

// TenantBindArgs returns the bind statement and args. Run it in the
// same transaction as subsequent queries so SET LOCAL applies.
func TenantBindArgs(t tenancy.Context) (query string, args []any, err error) {
	if err := t.Validate(); err != nil {
		return "", nil, err
	}
	if t.WorkspaceID != "" {
		return TenantWorkspaceStmtSQL, []any{t.TenantID, t.WorkspaceID}, nil
	}
	return TenantStmtSQL, []any{t.TenantID}, nil
}
