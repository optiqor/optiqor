-- +goose Up
-- +goose StatementBegin
--
-- Additive columns on `workloads` for current observed state.
--
-- These columns are written by the in-cluster agent reconcile loop
-- (Phase 5). They are populated **per workload** as the agent's K8s
-- informer reports the current pod template; they replace the need
-- for an extra `workload_observed_state` join table by keeping the
-- single-writer denormalised model captured in backend/todo.md §
-- "Design call: denormalised current-state on workloads".
--
-- `container_image` is **non-negotiable from row one** — it powers
-- the Helm Chart Efficiency Leaderboard and the cross-customer
-- pattern-library moat (queried under is_superuser_context() once
-- 0003 lands). It is the single most-expensive retrofit, so it ships
-- in Phase 1 follow-up before the agent writes the first row.
--
-- Default is denormalise (one writer = agent reconciler; dashboard
-- query speed wins; single source of truth = (workload_id, last_observed_at)).
-- Revisit only if dashboard latency budget bites.
--
-- All columns are NULL-able: workloads created from the GitOps parse
-- path (Phase 1) will not have observed state until the agent runs.

ALTER TABLE workloads
    ADD COLUMN container_image                   TEXT,
    ADD COLUMN current_cpu_request_millicores    INTEGER,
    ADD COLUMN current_memory_request_bytes      BIGINT,
    ADD COLUMN current_cpu_limit_millicores      INTEGER,
    ADD COLUMN current_memory_limit_bytes        BIGINT,
    ADD COLUMN replicas                          INTEGER,
    ADD COLUMN has_hpa                           BOOLEAN,
    ADD COLUMN last_observed_at                  TIMESTAMPTZ;

-- Cross-tenant pattern queries (Leaderboard, pattern library) read
-- this index under is_superuser_context() — see 0003. Tenant queries
-- still go through the tenant_idx; this index is the moat-enabler.
CREATE INDEX workloads_container_image_idx ON workloads (container_image)
    WHERE container_image IS NOT NULL;

-- Latency-sensitive dashboard query: "workloads observed in the last
-- 10m for this tenant". Partial index keeps it tight.
CREATE INDEX workloads_last_observed_idx ON workloads (tenant_id, last_observed_at DESC)
    WHERE last_observed_at IS NOT NULL;

-- Sanity checks on numeric ranges. Liberal upper bounds so we can
-- store oversized declarations and have the cost engine flag them
-- without the migration rejecting the row.
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
