-- +goose Up
-- +goose StatementBegin
--
-- vcs_installations holds one row per GitHub (or future GitLab) App
-- installation per tenant. installation_id is the provider's stable
-- handle (GitHub returns int64).
--
-- access_token_ciphertext stores the short-lived provider token
-- KMS-encrypted; plaintext is only decrypted inside a Temporal workflow
-- context that holds the KMS Decrypt grant. Tokens refresh in-place via
-- UPDATE; the row id stays stable so audit-log references survive.
--
-- Gates real PR-opening (Phase 4). Until installations land the
-- apply_fix workflow stays preview-only.

CREATE TABLE vcs_installations (
    id                       UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id                UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider                 TEXT NOT NULL CHECK (provider IN ('github','gitlab')),
    installation_id          BIGINT NOT NULL,
    account_login            TEXT NOT NULL,
    access_token_ciphertext  BYTEA,
    token_expires_at         TIMESTAMPTZ,
    installed_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at               TIMESTAMPTZ,
    status                   TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','revoked')),
    metadata                 JSONB NOT NULL DEFAULT '{}'::jsonb,
    UNIQUE (provider, installation_id)
);
CREATE INDEX vcs_installations_tenant_idx ON vcs_installations (tenant_id);
CREATE INDEX vcs_installations_active_idx ON vcs_installations (tenant_id, provider)
    WHERE status = 'active';

ALTER TABLE vcs_installations ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON vcs_installations
    USING (tenant_id::text = current_setting('app.tenant_id', true));

GRANT SELECT, INSERT, UPDATE ON vcs_installations TO optiqor_app;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP POLICY IF EXISTS tenant_isolation ON vcs_installations;
DROP INDEX IF EXISTS vcs_installations_active_idx;
DROP INDEX IF EXISTS vcs_installations_tenant_idx;
DROP TABLE IF EXISTS vcs_installations;
-- +goose StatementEnd
