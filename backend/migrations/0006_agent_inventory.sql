-- +goose Up
-- +goose StatementBegin
--
-- Phase 5 agent identity + inventory.
--
-- tenants.spiffe_id binds the in-cluster agent's mTLS client cert to a
-- tenant. The SPIFFE URI SAN takes the form
-- spiffe://optiqor.dev/tenant/<uuid>, which the api's mtls extractor
-- parses to set app.tenant_id. UNIQUE because two tenants must not
-- share an SVID — a duplicate would let one tenant write into the
-- other's table set on every POST.
--
-- agents is the dashboard's source for "agent health": last_seen_at
-- drives the "data freshness" pill, status surfaces an outage. RLS-
-- scoped per tenant; a single tenant can register many agents
-- (multi-cluster).

ALTER TABLE tenants ADD COLUMN spiffe_id TEXT UNIQUE;
COMMENT ON COLUMN tenants.spiffe_id IS
    'SPIFFE URI SAN bound to the agent mTLS client cert. Format: '
    'spiffe://optiqor.dev/tenant/<uuid>. Set at agent install; never '
    'rotate without the agent rotating its cert in the same change.';

CREATE TABLE agents (
    id                    UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id             UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cluster_id            UUID REFERENCES clusters(id) ON DELETE SET NULL,
    version               TEXT NOT NULL,
    last_seen_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    status                TEXT NOT NULL DEFAULT 'healthy' CHECK (status IN ('healthy','degraded','offline')),
    data_freshness_seconds INTEGER NOT NULL DEFAULT 0,
    installed_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX agents_tenant_idx ON agents (tenant_id);
CREATE INDEX agents_last_seen_idx ON agents (last_seen_at DESC);

ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON agents
    USING (tenant_id::text = current_setting('app.tenant_id', true));

GRANT SELECT, INSERT, UPDATE ON agents TO optiqor_app;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP POLICY IF EXISTS tenant_isolation ON agents;
DROP INDEX IF EXISTS agents_last_seen_idx;
DROP INDEX IF EXISTS agents_tenant_idx;
DROP TABLE IF EXISTS agents;

ALTER TABLE tenants DROP COLUMN IF EXISTS spiffe_id;
-- +goose StatementEnd
