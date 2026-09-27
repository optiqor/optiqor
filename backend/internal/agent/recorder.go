package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/optiqor/optiqor/internal/platform/db"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// PgRecorder writes every LLM call to llm_calls under tenant RLS. One
// BEGIN per call — the volume is bounded by the $0.40 cap so the
// transaction overhead is acceptable for the audit trail it buys.
type PgRecorder struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

func NewPgRecorder(pool *pgxpool.Pool) *PgRecorder {
	return &PgRecorder{Pool: pool}
}

var _ BudgetRecorder = (*PgRecorder)(nil)

const insertLLMCall = `
INSERT INTO llm_calls (
    tenant_id, model, purpose, input_hash, output_hash,
    input_tokens, output_tokens, cache_hit_tokens,
    cost_usd_cents, duration_ms, called_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`

func (r *PgRecorder) Record(ctx context.Context, t tenancy.Context, c CallRecord) error {
	if r == nil || r.Pool == nil {
		return nil
	}
	if err := t.Validate(); err != nil {
		return err
	}

	bindSQL, bindArgs, err := db.TenantBindArgs(t)
	if err != nil {
		return fmt.Errorf("agent/recorder: tenant bind: %w", err)
	}

	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("agent/recorder: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, bindSQL, bindArgs...); err != nil {
		return fmt.Errorf("agent/recorder: bind: %w", err)
	}

	purpose := c.Purpose
	if purpose == "" {
		purpose = "apply-fix"
	}

	_, err = tx.Exec(ctx, insertLLMCall,
		t.TenantID,
		modelEnum(c.Model),
		purpose,
		c.InputHash[:],
		c.OutputHash[:],
		c.InputTokens,
		c.OutputTokens,
		c.CacheHitTokens,
		c.CostUSDCents,
		c.DurationMS,
		r.now(),
	)
	if err != nil {
		return fmt.Errorf("agent/recorder: insert: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agent/recorder: commit: %w", err)
	}
	return nil
}

func (r *PgRecorder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}

// modelEnum maps the Anthropic model id back to the llm_calls.model
// CHECK constraint (haiku|sonnet|opus|other). Unknown providers fall
// through to "other" so the INSERT never trips the CHECK.
func modelEnum(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "haiku"):
		return "haiku"
	case strings.Contains(m, "sonnet"):
		return "sonnet"
	case strings.Contains(m, "opus"):
		return "opus"
	}
	return "other"
}
