package ingestion

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

// AgentSnapshotPgSink persists each verified AgentSnapshot. Two writes
// per snapshot: one upsert on agents to refresh last_seen_at + status
// + data freshness, and one conditional UPDATE on clusters to publish
// the detected provisioner class. Both run RLS-bound inside a single
// transaction so a partial failure leaves no orphan row.
type AgentSnapshotPgSink struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

func NewAgentSnapshotPgSink(p *pgxpool.Pool) *AgentSnapshotPgSink {
	return &AgentSnapshotPgSink{Pool: p}
}

var _ AgentSnapshotSink = (*AgentSnapshotPgSink)(nil)

// agentsUpsertSQL is one atomic statement guarded by the partial unique
// index (tenant_id, cluster_id) from migration 0010. The pre-0010
// refresh-then-insert pattern raced when two snapshots landed
// simultaneously; ON CONFLICT DO UPDATE removes the window.
const agentsUpsertSQL = `
INSERT INTO agents (tenant_id, cluster_id, version, last_seen_at, status, data_freshness_seconds)
VALUES ($1, $2::uuid, $3, $4, $5, $6)
ON CONFLICT (tenant_id, cluster_id) WHERE cluster_id IS NOT NULL
DO UPDATE SET
    version                = EXCLUDED.version,
    last_seen_at           = EXCLUDED.last_seen_at,
    status                 = EXCLUDED.status,
    data_freshness_seconds = EXCLUDED.data_freshness_seconds`

const clustersClassUpdateSQL = `
UPDATE clusters
   SET node_provisioner_class = $2
 WHERE id = $1::uuid
   AND tenant_id::text = current_setting('app.tenant_id', true)
   AND (node_provisioner_class IS DISTINCT FROM $2)`

func (s *AgentSnapshotPgSink) Persist(t tenancy.Context, snap AgentSnapshot) error {
	if s == nil || s.Pool == nil {
		return errors.New("ingestion/sink: nil pool")
	}
	if err := t.Validate(); err != nil {
		return err
	}
	bindSQL, bindArgs, err := db.TenantBindArgs(t)
	if err != nil {
		return fmt.Errorf("ingestion/sink: tenant bind: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("ingestion/sink: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, bindSQL, bindArgs...); err != nil {
		return fmt.Errorf("ingestion/sink: bind: %w", err)
	}

	clusterID := snap.ClusterID
	if clusterID == "" {
		return errors.New("ingestion/sink: snapshot has no cluster_id")
	}
	status := classifyStatus(snap)
	freshness := 0
	if snap.Health != nil {
		freshness = snap.Health.DataFreshnessSeconds
	}
	now := s.now(snap.CapturedAt)

	if _, err := tx.Exec(ctx, agentsUpsertSQL,
		t.TenantID, clusterID, snap.AgentVersion, now, status, freshness,
	); err != nil {
		return fmt.Errorf("ingestion/sink: upsert agents: %w", err)
	}

	if snap.ProvisionerClass != "" {
		if _, err := tx.Exec(ctx, clustersClassUpdateSQL, clusterID, snap.ProvisionerClass); err != nil {
			return fmt.Errorf("ingestion/sink: update clusters: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (s *AgentSnapshotPgSink) now(captured time.Time) time.Time {
	if !captured.IsZero() {
		return captured.UTC()
	}
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// classifyStatus maps health-pill thresholds to the agents.status CHECK
// constraint. Mirrors the web AgentHealthPill: < 60min healthy, < 6h
// degraded, otherwise offline. Kept here so the worker doesn't need to
// import dashboard internals.
func classifyStatus(snap AgentSnapshot) string {
	if snap.Health == nil {
		return "healthy"
	}
	switch {
	case snap.Health.DataFreshnessSeconds > 6*3600:
		return "offline"
	case snap.Health.DataFreshnessSeconds > 60*60:
		return "degraded"
	default:
		return "healthy"
	}
}
