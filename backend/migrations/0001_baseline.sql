-- +goose Up
-- +goose StatementBegin
--
-- Baseline schema. Locks the four-level tenants → workspaces →
-- clusters → namespaces → workloads hierarchy; retrofitting it
-- post-launch is a migration we don't want to write.
--
-- RLS is enforced server-side on every tenant-scoped table by reading
-- current_setting('app.tenant_id'). App code MUST run inside a
-- transaction that calls set_config('app.tenant_id', $1, true);
-- internal/platform/db owns the bind helpers. Variable name is
-- load-bearing — see 0003.

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

-- optiqor_migrator BYPASSRLS so migrations and platform jobs can read
-- across tenants; optiqor_app is RLS-subject so app code can't. Real
-- deployments provision both via Terraform; this DO-block keeps local
-- goose runs working without it.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'optiqor_app') THEN
        CREATE ROLE optiqor_app NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'optiqor_migrator') THEN
        CREATE ROLE optiqor_migrator NOLOGIN BYPASSRLS;
    END IF;
END
$$;

-- ---------------------------------------------------------------
-- tenants
-- ---------------------------------------------------------------
-- Region is restricted to Y1 deploy regions; expanding it needs an
-- explicit migration so an SDK default can't quietly land customer
-- data in a region we haven't legally cleared.
CREATE TABLE tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        CITEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    plan        TEXT NOT NULL DEFAULT 'free' CHECK (plan IN ('free','team','enterprise')),
    region      TEXT NOT NULL DEFAULT 'us-east-1' CHECK (region IN ('us-east-1','eu-west-1')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    -- JSONB so intermediate onboarding states can land without a
    -- schema migration.
    onboarding_state JSONB NOT NULL DEFAULT '{}'::jsonb
);

-- tenants is NOT RLS-scoped — it's the lookup surface for resolving
-- the scope. App-layer authz gates direct reads.

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
    -- Gates sizing strategy: Karpenter > CAS+ASG > static > managed.
    node_provisioner_class TEXT CHECK (node_provisioner_class IN ('karpenter','autoscaler','static','managed-aks','managed-gke','managed-hetzner')),
    -- "unknown" gets prod treatment server-side (fail-safe).
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
    -- Extracted from labels per workspace config.
    team        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (cluster_id, name)
);
CREATE INDEX namespaces_tenant_idx ON namespaces (tenant_id);

-- ---------------------------------------------------------------
-- workloads
-- ---------------------------------------------------------------
-- workload_hash is the selector-based identity: sha256(cluster_id ||
-- namespace || kind || canonical(primary_selector_labels)). Survives
-- renames and recreations, computed by the agent. Changing the input
-- set silently re-identifies every workload — don't.
CREATE TABLE workloads (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cluster_id    UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    namespace_id  UUID NOT NULL REFERENCES namespaces(id) ON DELETE CASCADE,
    workload_hash BYTEA NOT NULL,
    -- Same Deployment/api across 5 clusters shares one class group so
    -- fleet-wide Apply Fix applies.
    workload_class_group_id UUID,
    name          TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('Deployment','StatefulSet','DaemonSet','CronJob','Job','Other')),
    -- NULL for direct workloads; "operator:<group>/<kind>" for operator-owned.
    owner_kind    TEXT,
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
    -- Nullable for auto-expiry rows that have no human actor.
    dismissed_by      UUID,
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
    -- Canonical JSON, signed verbatim. Stored as TEXT (not JSONB) so the
    -- bytes round-trip unchanged — JSONB reformats on read (whitespace
    -- collapse, field reorder, number-precision) which would silently
    -- break Ed25519 signature verification. Cast to jsonb at query time
    -- if leaderboard / pattern-library queries need structural ops.
    payload         TEXT NOT NULL,
    signature       BYTEA NOT NULL,
    signing_key_id  TEXT NOT NULL,
    tlog_index      BIGINT,
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
    purpose     TEXT NOT NULL,
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
-- Applied uniformly to every tenant-scoped table. optiqor_migrator
-- bypasses; optiqor_app does not. This is the load-bearing tenant
-- isolation primitive — see CLAUDE.md "Multi-tenancy".

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

-- audit_log is append-only for the app role: every state-changing
-- action should be evidentiary, not editable post-hoc.
REVOKE UPDATE, DELETE ON audit_log FROM PUBLIC;
GRANT INSERT, SELECT ON audit_log TO optiqor_app;

GRANT USAGE ON SCHEMA public TO optiqor_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO optiqor_app;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA public TO optiqor_app;

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

DROP ROLE IF EXISTS optiqor_app;
DROP ROLE IF EXISTS optiqor_migrator;
-- +goose StatementEnd
