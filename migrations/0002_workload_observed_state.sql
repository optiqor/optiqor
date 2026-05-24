-- +goose Up
-- +goose StatementBegin
--
-- Additive observed-state columns on workloads. Written by the
-- in-cluster agent reconcile loop (Phase 5); single-writer denormalised
-- so the dashboard skips a join. All columns are NULL-able because
-- workloads from the GitOps parse path won't have observed state until
-- the agent runs. See backend/todo.md "Design call: denormalised
-- current-state on workloads".
--
-- container_image ships in row one because it powers the Helm Chart
-- Efficiency Leaderboard / cross-customer pattern library moat (queried
-- under is_superuser_context() once 0003 lands), and is the most
-- expensive column to retrofit after rows exist.

ALTER TABLE workloads
    ADD COLUMN container_image                   TEXT,
    ADD COLUMN current_cpu_request_millicores    INTEGER,
    ADD COLUMN current_memory_request_bytes      BIGINT,
    ADD COLUMN current_cpu_limit_millicores      INTEGER,
    ADD COLUMN current_memory_limit_bytes        BIGINT,
    ADD COLUMN replicas                          INTEGER,
    ADD COLUMN has_hpa                           BOOLEAN,
    ADD COLUMN last_observed_at                  TIMESTAMPTZ;

-- Read under is_superuser_context() by Leaderboard / pattern-library
-- queries (see 0003). Tenant queries keep using tenant_idx.
CREATE INDEX workloads_container_image_idx ON workloads (container_image)
    WHERE container_image IS NOT NULL;

-- Backs "workloads observed in the last 10m for this tenant" on the
-- dashboard hot path.
CREATE INDEX workloads_last_observed_idx ON workloads (tenant_id, last_observed_at DESC)
    WHERE last_observed_at IS NOT NULL;

-- No upper bound on these checks: oversized declarations are a finding
-- for the cost engine, not a migration-time reject.
ALTER TABLE workloads
    ADD CONSTRAINT workloads_current_cpu_request_nonneg
        CHECK (current_cpu_request_millicores IS NULL OR current_cpu_request_millicores >= 0),
    ADD CONSTRAINT workloads_current_memory_request_nonneg
        CHECK (current_memory_request_bytes IS NULL OR current_memory_request_bytes >= 0),
    ADD CONSTRAINT workloads_current_cpu_limit_nonneg
        CHECK (current_cpu_limit_millicores IS NULL OR current_cpu_limit_millicores >= 0),
    ADD CONSTRAINT workloads_current_memory_limit_nonneg
        CHECK (current_memory_limit_bytes IS NULL OR current_memory_limit_bytes >= 0),
    ADD CONSTRAINT workloads_replicas_nonneg
        CHECK (replicas IS NULL OR replicas >= 0);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE workloads
    DROP CONSTRAINT IF EXISTS workloads_replicas_nonneg,
    DROP CONSTRAINT IF EXISTS workloads_current_memory_limit_nonneg,
    DROP CONSTRAINT IF EXISTS workloads_current_cpu_limit_nonneg,
    DROP CONSTRAINT IF EXISTS workloads_current_memory_request_nonneg,
    DROP CONSTRAINT IF EXISTS workloads_current_cpu_request_nonneg;

DROP INDEX IF EXISTS workloads_last_observed_idx;
DROP INDEX IF EXISTS workloads_container_image_idx;

ALTER TABLE workloads
    DROP COLUMN IF EXISTS last_observed_at,
    DROP COLUMN IF EXISTS has_hpa,
    DROP COLUMN IF EXISTS replicas,
    DROP COLUMN IF EXISTS current_memory_limit_bytes,
    DROP COLUMN IF EXISTS current_cpu_limit_millicores,
    DROP COLUMN IF EXISTS current_memory_request_bytes,
    DROP COLUMN IF EXISTS current_cpu_request_millicores,
    DROP COLUMN IF EXISTS container_image;
-- +goose StatementEnd
