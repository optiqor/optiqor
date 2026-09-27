package dashboard

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

// AgentHealthPgStore reads the freshest agents row per tenant for the
// dashboard's Agent Health pill. The AgentSnapshotPgSink keeps this
// row warm on every snapshot tick; this reader is the projection.
type AgentHealthPgStore struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

func NewAgentHealthPgStore(p *pgxpool.Pool) *AgentHealthPgStore {
	return &AgentHealthPgStore{Pool: p}
}

var _ AgentHealthSource = (*AgentHealthPgStore)(nil)

const latestAgentSQL = `
SELECT status, last_seen_at, data_freshness_seconds, version
  FROM agents
 ORDER BY last_seen_at DESC
 LIMIT 1`

// Latest projects the freshest registered agent. Returns AgentHealth
// with status "offline" + zero LastCheckin when no agent has ever
// registered — the dashboard renders that as "install the agent"
// rather than treating it as an error.
func (s *AgentHealthPgStore) Latest(t tenancy.Context) (AgentHealth, error) {
	if s == nil || s.Pool == nil {
		return AgentHealth{Status: "offline"}, errors.New("dashboard/agent_pg: nil pool")
	}
	if err := t.Validate(); err != nil {
		return AgentHealth{}, err
	}
	bindSQL, bindArgs, err := db.TenantBindArgs(t)
	if err != nil {
		return AgentHealth{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return AgentHealth{}, fmt.Errorf("dashboard/agent_pg: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, bindSQL, bindArgs...); err != nil {
		return AgentHealth{}, fmt.Errorf("dashboard/agent_pg: bind: %w", err)
	}

	var (
		status    string
		lastSeen  time.Time
		freshness int
		version   *string
	)
	err = tx.QueryRow(ctx, latestAgentSQL).Scan(&status, &lastSeen, &freshness, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentHealth{Status: "offline"}, nil
	}
	if err != nil {
		return AgentHealth{}, fmt.Errorf("dashboard/agent_pg: scan: %w", err)
	}

	live := s.recomputeFreshness(lastSeen, freshness)
	out := AgentHealth{
		Status:               degradeStatus(status, live),
		LastCheckin:          lastSeen,
		DataFreshnessSeconds: live,
	}
	if version != nil {
		out.Version = *version
	}
	return out, nil
}

// recomputeFreshness overrides the agent's self-reported freshness
// with the wall-clock gap since last_seen_at when that gap is larger.
// Catches the case where the agent stopped checking in entirely — the
// agent's stored freshness would be stale otherwise.
func (s *AgentHealthPgStore) recomputeFreshness(lastSeen time.Time, reported int) int {
	now := s.now()
	wall := int(now.Sub(lastSeen).Seconds())
	if wall < 0 {
		wall = 0
	}
	if wall > reported {
		return wall
	}
	return reported
}

// degradeStatus flips a stored "healthy" to degraded/offline when the
// wall-clock freshness has already crossed the threshold. The stored
// status reflects the snapshot at write-time and goes stale; the pill
// must show the live picture or it lies about cluster state.
func degradeStatus(stored string, freshSeconds int) string {
	switch {
	case freshSeconds > 6*3600:
		return "offline"
	case freshSeconds > 60*60:
		if stored == "healthy" {
			return "degraded"
		}
	}
	return stored
}

func (s *AgentHealthPgStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
