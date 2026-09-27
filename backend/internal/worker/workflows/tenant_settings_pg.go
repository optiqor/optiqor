package workflows

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/optiqor/optiqor/internal/platform/db"
	"github.com/optiqor/optiqor/internal/tenancy"
)

type TenantSettingsPgSource struct {
	Pool *pgxpool.Pool
}

func NewTenantSettingsPgSource(p *pgxpool.Pool) *TenantSettingsPgSource {
	return &TenantSettingsPgSource{Pool: p}
}

var _ TenantSettingsSource = (*TenantSettingsPgSource)(nil)

const tenantSkepticSQL = `
SELECT skeptic_mode_default
  FROM tenants
 WHERE id::text = current_setting('app.tenant_id', true)
 LIMIT 1`

// SkepticModeDefault returns the per-tenant skeptic_mode_default flag.
// ErrNoRows degrades silently (caller falls back to the worker default)
// because a tenant with no row is either a fresh boot ahead of
// onboarding or a test seam; both cases want the safest worker default
// rather than a hard rejection.
func (s *TenantSettingsPgSource) SkepticModeDefault(ctx context.Context, t tenancy.Context) (bool, error) {
	if s == nil || s.Pool == nil {
		return false, errors.New("worker/tenant_settings: nil pool")
	}
	if err := t.Validate(); err != nil {
		return false, err
	}
	bindSQL, bindArgs, err := db.TenantBindArgs(t)
	if err != nil {
		return false, err
	}

	rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	tx, err := s.Pool.BeginTx(rctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return false, fmt.Errorf("worker/tenant_settings: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(rctx) }()

	if _, err := tx.Exec(rctx, bindSQL, bindArgs...); err != nil {
		return false, fmt.Errorf("worker/tenant_settings: bind: %w", err)
	}

	var v bool
	err = tx.QueryRow(rctx, tenantSkepticSQL).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, pgx.ErrNoRows
	}
	if err != nil {
		return false, fmt.Errorf("worker/tenant_settings: scan: %w", err)
	}
	return v, nil
}
