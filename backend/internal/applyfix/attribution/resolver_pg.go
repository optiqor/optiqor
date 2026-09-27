package attribution

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgResolver looks up tenant_id from a (provider, installation_id)
// pair. The lookup is cross-tenant by definition (we don't yet know
// the tenant), so it runs under set_superuser_context with a fixed
// audit reason. Every call writes an audit_log row via the function
// itself — the audit trail is non-negotiable.
type PgResolver struct {
	Pool *pgxpool.Pool
}

func NewPgResolver(p *pgxpool.Pool) *PgResolver { return &PgResolver{Pool: p} }

var _ TenantResolver = (*PgResolver)(nil)

const setSuperuserOn = `SELECT set_superuser_context(true, $1)`
const setSuperuserOff = `SELECT set_superuser_context(false, $1)`

const installationLookup = `
SELECT tenant_id::text FROM vcs_installations
 WHERE provider = $1
   AND installation_id = $2
   AND status = 'active'
 LIMIT 1`

func (r *PgResolver) ResolveInstallation(ctx context.Context, provider string, installationID int64) (string, error) {
	if provider == "" || installationID == 0 {
		return "", fmt.Errorf("attribution: empty provider or installation id")
	}

	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("attribution/resolver: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reason := fmt.Sprintf("vcs webhook resolve installation %s/%d", provider, installationID)
	if _, err := tx.Exec(ctx, setSuperuserOn, reason); err != nil {
		return "", fmt.Errorf("attribution/resolver: superuser on: %w", err)
	}

	var tenantID string
	err = tx.QueryRow(ctx, installationLookup, provider, installationID).Scan(&tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrUnknownInstallation
		}
		return "", fmt.Errorf("attribution/resolver: scan: %w", err)
	}

	// Flip back before commit so a stray query later in the same tx is
	// not cross-tenant by accident. The audit_log row from `on` stays.
	if _, err := tx.Exec(ctx, setSuperuserOff, reason); err != nil {
		return "", fmt.Errorf("attribution/resolver: superuser off: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("attribution/resolver: commit: %w", err)
	}
	return tenantID, nil
}
