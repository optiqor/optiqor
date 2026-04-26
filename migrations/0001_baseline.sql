-- +goose Up
-- +goose StatementBegin
--
-- Baseline schema for Sevro.
--
-- Models the four-level multi-cluster hierarchy that drives every
-- domain query and the audit / receipts surface. The schema is
-- locked in Phase 1 because retrofitting it after 50+ paying
-- customers requires an awful migration window — see
-- docs/strategy/business_strategy.md amendments.
--
--   tenants      legal entity / billing customer
--      ↓
--   workspaces   logical groupings inside a tenant
--      ↓
--   clusters     physical K8s clusters; own provisioner class, region, billing source
--      ↓
--   namespaces   K8s namespace; team mapping comes from labels
--      ↓
--   workloads    Deployment / StatefulSet / DaemonSet, identified by stable selector hash
--
-- Row-Level Security (RLS) is enforced server-side on every
-- tenant-scoped table by reading current_setting('app.tenant_id').
-- App code MUST run inside a transaction that calls
-- set_config('app.tenant_id', $1, true) at start; the
-- internal/platform/db package provides the bind helpers.

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

-- Two roles: a privileged migration role (bypasses RLS) and a
-- restricted app role (subject to RLS). Real deployments create
-- these via Terraform; the migration creates them only when
-- absent so local goose runs work too.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'sevro_app') THEN
        CREATE ROLE sevro_app NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'sevro_migrator') THEN
        CREATE ROLE sevro_migrator NOLOGIN BYPASSRLS;
    END IF;
END
$$;

-- ---------------------------------------------------------------
-- tenants
-- ---------------------------------------------------------------
CREATE TABLE tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        CITEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    plan        TEXT NOT NULL DEFAULT 'free' CHECK (plan IN ('free','team','enterprise')),
    region      TEXT NOT NULL DEFAULT 'us-east-1' CHECK (region IN ('us-east-1','eu-west-1')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    -- Onboarding state machine; the timestamps live in JSONB so we can
    -- add intermediate states without a schema migration.
    onboarding_state JSONB NOT NULL DEFAULT '{}'::jsonb
);

-- The tenants table itself is NOT RLS-scoped: it's the lookup
-- surface for resolving the scope. Direct queries against it are
-- gated by application-layer authorisation only.

-- ---------------------------------------------------------------
-- workspaces
-- ---------------------------------------------------------------
CREATE TABLE workspaces (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    slug        CITEXT NOT NULL,
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, slug)
);
CREATE INDEX workspaces_tenant_idx ON workspaces (tenant_id);

-- ---------------------------------------------------------------
-- clusters
-- ---------------------------------------------------------------
CREATE TABLE clusters (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id  UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    cloud         TEXT NOT NULL CHECK (cloud IN ('aws','azure','hetzner','onprem')),
    region        TEXT NOT NULL,
    -- Detected at agent install via pre-flight; gates which sizing
    -- strategy is allowed (Karpenter > CAS+ASG > static > managed).
    node_provisioner_class TEXT CHECK (node_provisioner_class IN ('karpenter','autoscaler','static','managed-aks','managed-gke','managed-hetzner')),
    -- Detected from labels and customer-configured rules; "unknown"
    -- defaults to "prod" treatment server-side (fail-safe).
    environment   TEXT NOT NULL DEFAULT 'unknown' CHECK (environment IN ('prod','staging','dev','unknown')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ,
    UNIQUE (tenant_id, workspace_id, name)
);
CREATE INDEX clusters_tenant_idx ON clusters (tenant_id);
CREATE INDEX clusters_workspace_idx ON clusters (workspace_id);

-- ---------------------------------------------------------------
-- namespaces
-- ---------------------------------------------------------------
CREATE TABLE namespaces (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cluster_id  UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    -- "team" extracted from labels per workspace config.
    team        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (cluster_id, name)
);
CREATE INDEX namespaces_tenant_idx ON namespaces (tenant_id);

-- ---------------------------------------------------------------
-- workloads
-- ---------------------------------------------------------------
CREATE TABLE workloads (
    -- Stable identity = sha256(cluster_id || namespace || kind || canonical(primary_selector_labels)).
    -- Survives renames and recreations.
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cluster_id    UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace_id  UUID NOT NULL REFERENCES namespaces(id) ON DELETE CASCADE,
    -- The hash of the stable selector labels; computed by the agent.
    workload_hash BYTEA NOT NULL,
    -- Cross-cluster grouping: same `Deployment/api` in 5 clusters
    -- shares one class group so fleet-wide Apply Fix applies.
    workload_class_group_id UUID,
    name          TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('Deployment','StatefulSet','DaemonSet','CronJob','Job','Other')),
    -- Owner ref chain. NULL for direct workloads; "operator:<group>/<kind>" for operator-owned.
    owner_kind    TEXT,
    -- Workload classification (from internal/workload/classifier).
    workload_class TEXT CHECK (workload_class IN ('web-steady','worker-bursty','batch','stateful-db','ml-inference','unknown')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ,
    UNIQUE (cluster_id, workload_hash)
);
CREATE INDEX workloads_tenant_idx ON workloads (tenant_id);
CREATE INDEX workloads_namespace_idx ON workloads (namespace_id);
CREATE INDEX workloads_class_group_idx ON workloads (workload_class_group_id) WHERE workload_class_group_id IS NOT NULL;

-- ---------------------------------------------------------------
-- recommendations
-- ---------------------------------------------------------------
CREATE TABLE recommendations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workload_id   UUID NOT NULL REFERENCES workloads(id) ON DELETE CASCADE,
    detector_id   TEXT NOT NULL,
    severity      TEXT NOT NULL CHECK (severity IN ('HIGH','MED','LOW','INFO')),
    confidence    TEXT NOT NULL CHECK (confidence IN ('high','medium','low')),
    title         TEXT NOT NULL,
    detail        TEXT NOT NULL DEFAULT '',
    monthly_usd_cents BIGINT NOT NULL DEFAULT 0,
    blast_radius  SMALLINT NOT NULL DEFAULT 1 CHECK (blast_radius BETWEEN 1 AND 5),
    state         TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','snoozed','dismissed','ignored-workload','ignored-class','applied-externally','expired')),
    snoozed_until TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '60 days'
);
CREATE INDEX recommendations_tenant_idx ON recommendations (tenant_id);
CREATE INDEX recommendations_workload_idx ON recommendations (workload_id);
CREATE INDEX recommendations_state_idx ON recommendations (tenant_id, state);

-- ---------------------------------------------------------------
-- recommendations_dismissals (audit trail for detector tuning)
-- ---------------------------------------------------------------
CREATE TABLE recommendation_dismissals (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    recommendation_id UUID NOT NULL REFERENCES recommendations(id) ON DELETE CASCADE,
    reason            TEXT NOT NULL,
    dismissed_by      UUID, -- user id; nullable for auto-expiry
    dismissed_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX rec_dismissals_tenant_idx ON recommendation_dismissals (tenant_id);

-- ---------------------------------------------------------------
-- apply_fixes (PR open / merge / rollback events)
-- ---------------------------------------------------------------
CREATE TABLE apply_fixes (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    recommendation_id UUID NOT NULL REFERENCES recommendations(id) ON DELETE CASCADE,
    vcs               TEXT NOT NULL CHECK (vcs IN ('github','gitlab','bitbucket')),
    repo              TEXT NOT NULL,
    pr_number         INTEGER NOT NULL,
    pr_url            TEXT NOT NULL,
    state             TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open','merged','closed','rolled-back')),
    opened_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    merged_at         TIMESTAMPTZ,
    closed_at         TIMESTAMPTZ
);
CREATE INDEX apply_fixes_tenant_idx ON apply_fixes (tenant_id);

-- ---------------------------------------------------------------
-- receipts (Ed25519-signed savings proofs)
-- ---------------------------------------------------------------
CREATE TABLE receipts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    apply_fix_id    UUID REFERENCES apply_fixes(id) ON DELETE SET NULL,
    tier            TEXT NOT NULL CHECK (tier IN ('cloud','capacity','hybrid')),
    methodology     TEXT NOT NULL,           -- e.g. "hybrid_v1.2"
    window_start    TIMESTAMPTZ NOT NULL,
    window_end      TIMESTAMPTZ NOT NULL,
    predicted_usd_cents BIGINT NOT NULL DEFAULT 0,
    actual_usd_cents    BIGINT NOT NULL DEFAULT 0,
    -- Receipt YAML payload as canonicalised JSON; signed verbatim.
    payload         JSONB NOT NULL,
    signature       BYTEA NOT NULL,           -- ed25519 signature
    signing_key_id  TEXT NOT NULL,
    tlog_index      BIGINT,                   -- index in transparency log
    issued_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX receipts_tenant_idx ON receipts (tenant_id);
CREATE INDEX receipts_tlog_idx ON receipts (tlog_index) WHERE tlog_index IS NOT NULL;

-- ---------------------------------------------------------------
-- llm_calls (cost attribution + per-prompt audit log)
-- ---------------------------------------------------------------
CREATE TABLE llm_calls (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    model       TEXT NOT NULL CHECK (model IN ('haiku','sonnet','opus','other')),
    purpose     TEXT NOT NULL, -- e.g. "diff", "narrative", "qa"
    input_hash  BYTEA NOT NULL,
    output_hash BYTEA NOT NULL,
    input_tokens   INTEGER NOT NULL,
    output_tokens  INTEGER NOT NULL,
    cache_hit_tokens INTEGER NOT NULL DEFAULT 0,
    cost_usd_cents INTEGER NOT NULL,
    duration_ms    INTEGER NOT NULL,
    called_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX llm_calls_tenant_idx ON llm_calls (tenant_id);
CREATE INDEX llm_calls_called_at_idx ON llm_calls (called_at DESC);

-- ---------------------------------------------------------------
-- audit_log (every state-changing action, immutable, 7-year retention)
-- ---------------------------------------------------------------
CREATE TABLE audit_log (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('user','system','agent','webhook')),
    actor_id    TEXT,
    action      TEXT NOT NULL,
    resource    TEXT NOT NULL,
    resource_id TEXT,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_tenant_idx ON audit_log (tenant_id);
CREATE INDEX audit_log_occurred_at_idx ON audit_log (occurred_at DESC);

-- ---------------------------------------------------------------
-- RLS policies
-- ---------------------------------------------------------------
-- Apply uniformly to every tenant-scoped table. The migration role
-- bypasses RLS (BYPASSRLS); the app role does not.

ALTER TABLE workspaces                   ENABLE ROW LEVEL SECURITY;
ALTER TABLE clusters                     ENABLE ROW LEVEL SECURITY;
ALTER TABLE namespaces                   ENABLE ROW LEVEL SECURITY;
ALTER TABLE workloads                    ENABLE ROW LEVEL SECURITY;
ALTER TABLE recommendations              ENABLE ROW LEVEL SECURITY;
ALTER TABLE recommendation_dismissals    ENABLE ROW LEVEL SECURITY;
ALTER TABLE apply_fixes                  ENABLE ROW LEVEL SECURITY;
ALTER TABLE receipts                     ENABLE ROW LEVEL SECURITY;
ALTER TABLE llm_calls                    ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_log                    ENABLE ROW LEVEL SECURITY;

CREATE POLICY tenant_isolation ON workspaces
    USING (tenant_id::text = current_setting('app.tenant_id', true));
CREATE POLICY tenant_isolation ON clusters
    USING (tenant_id::text = current_setting('app.tenant_id', true));
CREATE POLICY tenant_isolation ON namespaces
    USING (tenant_id::text = current_setting('app.tenant_id', true));
CREATE POLICY tenant_isolation ON workloads
    USING (tenant_id::text = current_setting('app.tenant_id', true));
CREATE POLICY tenant_isolation ON recommendations
    USING (tenant_id::text = current_setting('app.tenant_id', true));
CREATE POLICY tenant_isolation ON recommendation_dismissals
    USING (tenant_id::text = current_setting('app.tenant_id', true));
CREATE POLICY tenant_isolation ON apply_fixes
    USING (tenant_id::text = current_setting('app.tenant_id', true));
CREATE POLICY tenant_isolation ON receipts
    USING (tenant_id::text = current_setting('app.tenant_id', true));
CREATE POLICY tenant_isolation ON llm_calls
    USING (tenant_id::text = current_setting('app.tenant_id', true));
CREATE POLICY tenant_isolation ON audit_log
    USING (tenant_id::text = current_setting('app.tenant_id', true));

-- Read-only-by-default insert protection on audit_log: rows can be
-- inserted but not updated or deleted by the app role.
REVOKE UPDATE, DELETE ON audit_log FROM PUBLIC;
GRANT INSERT, SELECT ON audit_log TO sevro_app;

-- App role only sees what RLS allows.
GRANT USAGE ON SCHEMA public TO sevro_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO sevro_app;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO sevro_app;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS audit_log               CASCADE;
DROP TABLE IF EXISTS llm_calls               CASCADE;
DROP TABLE IF EXISTS receipts                CASCADE;
DROP TABLE IF EXISTS apply_fixes             CASCADE;
DROP TABLE IF EXISTS recommendation_dismissals CASCADE;
DROP TABLE IF EXISTS recommendations         CASCADE;
DROP TABLE IF EXISTS workloads               CASCADE;
DROP TABLE IF EXISTS namespaces              CASCADE;
DROP TABLE IF EXISTS clusters                CASCADE;
DROP TABLE IF EXISTS workspaces              CASCADE;
DROP TABLE IF EXISTS tenants                 CASCADE;

DROP ROLE IF EXISTS sevro_app;
DROP ROLE IF EXISTS sevro_migrator;
-- +goose StatementEnd
