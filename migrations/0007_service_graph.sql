-- +goose Up
-- +goose StatementBegin
--
-- service_graph_snapshots holds the agent-derived workload→service
-- adjacency. One row per (tenant, cluster, batch). payload is the
-- agent's serialised graph.Snapshot — small enough (< 64 KiB on a
-- typical cluster) to keep inline.
--
-- 7-day retention via captured_at — anything older serves no
-- recommendation purpose. TimescaleDB hypertable when the extension
-- is available; plain table otherwise so dev/local doesn't need
-- TimescaleDB installed.

CREATE TABLE service_graph_snapshots (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cluster_id    UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    batch_id      TEXT NOT NULL,
    captured_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    workload_count INTEGER NOT NULL DEFAULT 0,
    edge_count    INTEGER NOT NULL DEFAULT 0,
    payload       JSONB NOT NULL,
    UNIQUE (tenant_id, cluster_id, batch_id)
);

CREATE INDEX service_graph_snapshots_tenant_idx
    ON service_graph_snapshots (tenant_id);
CREATE INDEX service_graph_snapshots_recent_idx
    ON service_graph_snapshots (tenant_id, captured_at DESC);

ALTER TABLE service_graph_snapshots ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON service_graph_snapshots
    USING (tenant_id::text = current_setting('app.tenant_id', true));

GRANT SELECT, INSERT, DELETE ON service_graph_snapshots TO optiqor_app;

-- Best-effort hypertable. The DO block degrades gracefully when
-- TimescaleDB is absent (local dev, test containers). Production
-- runs against RDS Postgres with the timescaledb extension pre-loaded.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
        PERFORM create_hypertable('service_graph_snapshots', 'captured_at',
            chunk_time_interval => INTERVAL '1 day',
            if_not_exists => TRUE);
        PERFORM add_retention_policy('service_graph_snapshots',
            INTERVAL '7 days', if_not_exists => TRUE);
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP POLICY IF EXISTS tenant_isolation ON service_graph_snapshots;
DROP INDEX IF EXISTS service_graph_snapshots_recent_idx;
DROP INDEX IF EXISTS service_graph_snapshots_tenant_idx;
DROP TABLE IF EXISTS service_graph_snapshots;
-- +goose StatementEnd
