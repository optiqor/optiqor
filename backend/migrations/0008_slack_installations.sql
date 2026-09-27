-- +goose Up
-- +goose StatementBegin
--
-- slack_installations holds one webhook URL per tenant. Phase 5 ships
-- the webhook-only model; Phase 7 adds OAuth + slash commands and
-- widens this table with bot_token / signing_secret columns.
--
-- webhook_url_ciphertext stores the URL KMS-encrypted; the worker
-- decrypts only inside the post path. Leaking the URL leaks tenant
-- digest content to anyone who can POST to it, so it gets the same
-- treatment as access tokens.

CREATE TABLE slack_installations (
    id                      UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id               UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id            UUID REFERENCES workspaces(id) ON DELETE SET NULL,
    webhook_url_ciphertext  BYTEA NOT NULL,
    channel                 TEXT,
    installed_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at              TIMESTAMPTZ,
    status                  TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
    UNIQUE (tenant_id)
);
CREATE INDEX slack_installations_tenant_idx
    ON slack_installations (tenant_id);

ALTER TABLE slack_installations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON slack_installations
    USING (tenant_id::text = current_setting('app.tenant_id', true));

GRANT SELECT, INSERT, UPDATE ON slack_installations TO optiqor_app;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP POLICY IF EXISTS tenant_isolation ON slack_installations;
DROP INDEX IF EXISTS slack_installations_tenant_idx;
DROP TABLE IF EXISTS slack_installations;
-- +goose StatementEnd
