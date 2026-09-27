-- +goose Up
-- +goose StatementBegin
--
-- Phase 5 audit follow-up: the AgentSnapshotPgSink's refresh-then-insert
-- pattern can race when two snapshots from the same agent land
-- simultaneously. Without a uniqueness key on (tenant_id, cluster_id)
-- both transactions miss the UPDATE and both INSERT, leaving duplicate
-- rows whose freshness disagrees.
--
-- A partial unique index covers the "non-null cluster_id" case (the
-- production path the agent's Helm chart bakes in). NULL cluster_id is
-- left unconstrained because two agents installed without a cluster
-- registration are legitimately distinct rows; the dashboard pill
-- picks the freshest via ORDER BY last_seen_at DESC.

CREATE UNIQUE INDEX IF NOT EXISTS agents_tenant_cluster_uniq
    ON agents (tenant_id, cluster_id)
    WHERE cluster_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS agents_tenant_cluster_uniq;
-- +goose StatementEnd
