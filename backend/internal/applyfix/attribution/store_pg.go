package attribution

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

// PgStore satisfies Store against apply_fixes + recommendations. Joins
// recommendations to read MonthlySavingsUSDCents so the post-merge
// comment quotes the same number the open-PR comment showed.
type PgStore struct {
	Pool *pgxpool.Pool
}

func NewPgStore(p *pgxpool.Pool) *PgStore { return &PgStore{Pool: p} }

var _ Store = (*PgStore)(nil)

const lookupSQL = `
SELECT af.id::text, af.tenant_id::text, af.pr_url,
       COALESCE(r.workload_id::text, ''),
       COALESCE(r.title, ''),
       COALESCE(r.monthly_usd_cents, 0)
  FROM apply_fixes af
  LEFT JOIN recommendations r ON r.id = af.recommendation_id
 WHERE af.vcs = 'github'
   AND af.repo = $1
   AND af.pr_number = $2
   AND af.state <> 'merged'`

func (s *PgStore) LookupByRepoPR(ctx context.Context, t tenancy.Context, repo string, prNumber int) (Row, error) {
	if err := t.Validate(); err != nil {
		return Row{}, err
	}
	bindSQL, bindArgs, err := db.TenantBindArgs(t)
	if err != nil {
		return Row{}, err
	}

	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return Row{}, fmt.Errorf("attribution/pg: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, bindSQL, bindArgs...); err != nil {
		return Row{}, fmt.Errorf("attribution/pg: bind: %w", err)
	}

	var row Row
	err = tx.QueryRow(ctx, lookupSQL, repo, prNumber).Scan(
		&row.ID, &row.TenantID, &row.PRURL,
		&row.Workload, &row.Title, &row.MonthlySavingsUSDCents,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Row{}, ErrNoMatchingApplyFix
		}
		return Row{}, fmt.Errorf("attribution/pg: scan: %w", err)
	}
	return row, nil
}

const markMergedSQL = `
UPDATE apply_fixes
   SET state = 'merged',
       merged_at = $2
 WHERE id = $1
   AND state <> 'merged'`

func (s *PgStore) MarkMerged(ctx context.Context, t tenancy.Context, id string, mergedAt time.Time) error {
	if err := t.Validate(); err != nil {
		return err
	}
	bindSQL, bindArgs, err := db.TenantBindArgs(t)
	if err != nil {
		return err
	}

	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("attribution/pg: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, bindSQL, bindArgs...); err != nil {
		return fmt.Errorf("attribution/pg: bind: %w", err)
	}
	tag, err := tx.Exec(ctx, markMergedSQL, id, mergedAt)
	if err != nil {
		return fmt.Errorf("attribution/pg: update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoMatchingApplyFix
	}
	return tx.Commit(ctx)
}
