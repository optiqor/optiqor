//go:build integration

package integration

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/optiqor/optiqor/internal/sandbox"
)

// PgxExec adapts pgxpool.Pool to sandbox.PgExec.
type PgxExec struct{ Pool *pgxpool.Pool }

func (p PgxExec) QueryRow(ctx context.Context, sql string, args ...any) sandbox.PgRow {
	return pgxRow{r: p.Pool.QueryRow(ctx, sql, args...)}
}

func (p PgxExec) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := p.Pool.Exec(ctx, sql, args...)
	return err
}

type pgxRow struct{ r pgx.Row }

func (r pgxRow) Scan(dest ...any) error {
	if err := r.r.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sandbox.ErrPgNoRows
		}
		return err
	}
	return nil
}
