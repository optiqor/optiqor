-- +goose Up
-- +goose StatementBegin
--
-- Tenancy primitives — additive helpers + pure refactor of the
-- baseline RLS policies. Semantics are unchanged for tenant-scoped
-- reads: a row is visible iff its tenant_id matches the current
-- session's app.tenant_id setting. The refactor exists to:
--
--   1. Hide the `current_setting('app.tenant_id', true)` cast behind
--      a single function so the bind contract is owned in one place.
--   2. Add a per-transaction superuser context flag (audited on every
--      flip) so cross-tenant aggregation jobs (Leaderboard, pattern
--      library) can run without forking the schema.
--   3. Add time-ordered UUIDs (v7) for append-heavy hot tables added
--      later (metric_samples, llm_calls follow-ups). Existing v4 IDs
--      stay — this is an *additional* generator, not a rename.
--   4. Add a standard set_updated_at() trigger so every table with an
--      `updated_at` column stays correct without app-level discipline.
--
-- The session variable name stays `app.tenant_id` to match the
-- internal/platform/db bind helper. Do not rename it without
-- updating the Go side in the same commit.
--
-- See docs/strategy/technical_implementation.md §4.2.2 for rationale.

-- ---------------------------------------------------------------
-- v7 UUID generator (time-ordered).
-- ---------------------------------------------------------------
-- RFC 9562 §5.7 layout: 48-bit unix-ms timestamp || 4-bit version=7
-- || 12 bits of random || 2-bit variant=10 || 62 bits of random.
-- Pure SQL, no extensions — keeps the migration portable across
-- managed Postgres (RDS, Aurora, Hetzner) without needing pg_uuidv7.
CREATE OR REPLACE FUNCTION uuid_generate_v7()
RETURNS UUID
LANGUAGE plpgsql
VOLATILE
PARALLEL SAFE
AS $$
DECLARE
    unix_ms        BIGINT;
    rand_bytes     BYTEA;
    uuid_bytes     BYTEA;
BEGIN
    unix_ms    := (EXTRACT(EPOCH FROM clock_timestamp()) * 1000)::BIGINT;
    rand_bytes := gen_random_bytes(10);

    -- 6 bytes timestamp (big-endian) || 10 bytes random
    uuid_bytes := set_byte(
                    set_byte(
                      set_byte(
                        set_byte(
                          set_byte(
                            set_byte(rand_bytes || rand_bytes, 0, ((unix_ms >> 40) & 255)::INTEGER),
                            1, ((unix_ms >> 32) & 255)::INTEGER),
                          2, ((unix_ms >> 24) & 255)::INTEGER),
                        3, ((unix_ms >> 16) & 255)::INTEGER),
                      4, ((unix_ms >> 8)  & 255)::INTEGER),
                    5, (unix_ms & 255)::INTEGER);

    uuid_bytes := substring(uuid_bytes FROM 1 FOR 16);

    -- Version = 7 in the high nibble of byte 6.
    uuid_bytes := set_byte(uuid_bytes, 6,
                    ((get_byte(uuid_bytes, 6) & 15) | 112));
    -- Variant = 10 in the high two bits of byte 8.
    uuid_bytes := set_byte(uuid_bytes, 8,
                    ((get_byte(uuid_bytes, 8) & 63) | 128));

    RETURN encode(uuid_bytes, 'hex')::UUID;
END
$$;

COMMENT ON FUNCTION uuid_generate_v7() IS
    'RFC 9562 v7 UUIDs. Time-ordered, safe for append-heavy hot tables. '
    'Existing baseline tables keep gen_random_uuid() (v4); v7 is opt-in '
    'per table by setting DEFAULT uuid_generate_v7().';

-- ---------------------------------------------------------------
-- current_tenant_id() — single source of truth for the bind.
-- ---------------------------------------------------------------
-- Reads the session variable set by internal/platform/db.BindTenant.
-- Returns NULL if unset, which (combined with the policy) means the
-- query sees zero rows — fail-closed.
CREATE OR REPLACE FUNCTION current_tenant_id()
RETURNS UUID
LANGUAGE sql
STABLE
PARALLEL SAFE
AS $$
    SELECT NULLIF(current_setting('app.tenant_id', true), '')::UUID
$$;

COMMENT ON FUNCTION current_tenant_id() IS
    'Returns the tenant UUID bound on the current session via '
    'set_config(''app.tenant_id'', $1, true). NULL when unset (RLS '
    'evaluates to deny). Variable name MUST stay app.tenant_id to '
    'match internal/platform/db.';

-- ---------------------------------------------------------------
-- is_superuser_context() — per-transaction bypass flag.
-- ---------------------------------------------------------------
-- Used by cross-tenant aggregation jobs (Helm Chart Efficiency
-- Leaderboard, pattern library, fleet-wide moat queries). The flag
-- must be flipped through set_superuser_context(), which writes an
-- audit_log row on every flip — direct set_config calls bypass the
-- audit and are caught by the gosec lint rule in CI.
CREATE OR REPLACE FUNCTION is_superuser_context()
RETURNS BOOLEAN
LANGUAGE sql
STABLE
PARALLEL SAFE
AS $$
    SELECT COALESCE(current_setting('app.superuser_context', true) = 'on', false)
$$;

COMMENT ON FUNCTION is_superuser_context() IS
    'Per-transaction flag. When true, tenant_isolation policies admit '
    'all rows. Flip ONLY through set_superuser_context(reason) so the '
    'audit_log row is written.';

CREATE OR REPLACE FUNCTION set_superuser_context(p_on BOOLEAN, p_reason TEXT)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    IF p_reason IS NULL OR length(trim(p_reason)) = 0 THEN
        RAISE EXCEPTION 'set_superuser_context: reason is required';
    END IF;

    PERFORM set_config('app.superuser_context', CASE WHEN p_on THEN 'on' ELSE 'off' END, true);

    -- Audit row uses NULL tenant_id intentionally (cross-tenant op).
    -- The audit_log RLS policy is rewritten below to allow inserts
    -- with NULL tenant_id when is_superuser_context() is true.
    INSERT INTO audit_log (tenant_id, actor_kind, actor_id, action, resource, metadata)
    VALUES (
        NULL,
        'system',
        current_user,
        CASE WHEN p_on THEN 'superuser_context.enable' ELSE 'superuser_context.disable' END,
        'session',
        jsonb_build_object('reason', p_reason)
    );
END
$$;

COMMENT ON FUNCTION set_superuser_context(BOOLEAN, TEXT) IS
    'Toggle cross-tenant access for the current transaction. Reason '
    'is mandatory and recorded in audit_log. Pair every enable with '
    'a disable in defer; the session-scoped flag does not survive '
    'commit but defensive disable keeps long transactions safe.';

-- Allow NULL tenant_id on audit_log so cross-tenant ops can audit.
ALTER TABLE audit_log
    ALTER COLUMN tenant_id DROP NOT NULL;

-- ---------------------------------------------------------------
-- set_updated_at() trigger — keep updated_at correct.
-- ---------------------------------------------------------------
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END
$$;

-- Attach to every baseline table that has an updated_at column.
CREATE TRIGGER tenants_set_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER workspaces_set_updated_at
    BEFORE UPDATE ON workspaces
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER recommendations_set_updated_at
    BEFORE UPDATE ON recommendations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------
-- Rewrite tenant_isolation policies to use the helpers.
-- ---------------------------------------------------------------
-- Semantics: row visible iff tenant matches OR superuser context.
-- The audit_log policy additionally admits NULL tenant_id under
-- superuser context so cross-tenant ops can record themselves.

DROP POLICY IF EXISTS tenant_isolation ON workspaces;
DROP POLICY IF EXISTS tenant_isolation ON clusters;
DROP POLICY IF EXISTS tenant_isolation ON namespaces;
DROP POLICY IF EXISTS tenant_isolation ON workloads;
DROP POLICY IF EXISTS tenant_isolation ON recommendations;
DROP POLICY IF EXISTS tenant_isolation ON recommendation_dismissals;
DROP POLICY IF EXISTS tenant_isolation ON apply_fixes;
DROP POLICY IF EXISTS tenant_isolation ON receipts;
DROP POLICY IF EXISTS tenant_isolation ON llm_calls;
DROP POLICY IF EXISTS tenant_isolation ON audit_log;

CREATE POLICY tenant_isolation ON workspaces
    USING (tenant_id = current_tenant_id() OR is_superuser_context());
CREATE POLICY tenant_isolation ON clusters
    USING (tenant_id = current_tenant_id() OR is_superuser_context());
CREATE POLICY tenant_isolation ON namespaces
    USING (tenant_id = current_tenant_id() OR is_superuser_context());
CREATE POLICY tenant_isolation ON workloads
    USING (tenant_id = current_tenant_id() OR is_superuser_context());
CREATE POLICY tenant_isolation ON recommendations
    USING (tenant_id = current_tenant_id() OR is_superuser_context());
CREATE POLICY tenant_isolation ON recommendation_dismissals
    USING (tenant_id = current_tenant_id() OR is_superuser_context());
CREATE POLICY tenant_isolation ON apply_fixes
    USING (tenant_id = current_tenant_id() OR is_superuser_context());
CREATE POLICY tenant_isolation ON receipts
    USING (tenant_id = current_tenant_id() OR is_superuser_context());
CREATE POLICY tenant_isolation ON llm_calls
    USING (tenant_id = current_tenant_id() OR is_superuser_context());
CREATE POLICY tenant_isolation ON audit_log
    USING (
        (tenant_id IS NOT NULL AND tenant_id = current_tenant_id())
        OR is_superuser_context()
    );

-- Grant execute on the helpers to the app role; the migrator role
-- already has it via owner privileges.
GRANT EXECUTE ON FUNCTION uuid_generate_v7()                TO optiqor_app;
GRANT EXECUTE ON FUNCTION current_tenant_id()               TO optiqor_app;
GRANT EXECUTE ON FUNCTION is_superuser_context()            TO optiqor_app;
GRANT EXECUTE ON FUNCTION set_superuser_context(BOOLEAN, TEXT) TO optiqor_app;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP POLICY IF EXISTS tenant_isolation ON audit_log;
DROP POLICY IF EXISTS tenant_isolation ON llm_calls;
DROP POLICY IF EXISTS tenant_isolation ON receipts;
DROP POLICY IF EXISTS tenant_isolation ON apply_fixes;
DROP POLICY IF EXISTS tenant_isolation ON recommendation_dismissals;
DROP POLICY IF EXISTS tenant_isolation ON recommendations;
DROP POLICY IF EXISTS tenant_isolation ON workloads;
DROP POLICY IF EXISTS tenant_isolation ON namespaces;
DROP POLICY IF EXISTS tenant_isolation ON clusters;
DROP POLICY IF EXISTS tenant_isolation ON workspaces;

-- Restore the baseline policies verbatim.
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

DROP TRIGGER IF EXISTS recommendations_set_updated_at ON recommendations;
DROP TRIGGER IF EXISTS workspaces_set_updated_at      ON workspaces;
DROP TRIGGER IF EXISTS tenants_set_updated_at         ON tenants;

DROP FUNCTION IF EXISTS set_updated_at();
DROP FUNCTION IF EXISTS set_superuser_context(BOOLEAN, TEXT);
DROP FUNCTION IF EXISTS is_superuser_context();
DROP FUNCTION IF EXISTS current_tenant_id();
DROP FUNCTION IF EXISTS uuid_generate_v7();

ALTER TABLE audit_log
    ALTER COLUMN tenant_id SET NOT NULL;
-- +goose StatementEnd
