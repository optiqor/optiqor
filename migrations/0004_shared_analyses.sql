-- +goose Up
-- +goose StatementBegin
--
-- Postgres backing for optiqor.dev/r/<hash>. Public by design — no RLS,
-- no tenant scope; the hash itself is the access token (96-bit sha256
-- prefix, see internal/sandbox.hashBytes). Payload sits in-row up to
-- ~256 KiB; a follow-up migration promotes larger bodies to S3 via
-- payload_s3_key, enforced by the XOR CHECK on this row.
--
-- Go binding: internal/sandbox/pg_store.go.

CREATE TABLE shared_analyses (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    hash            TEXT NOT NULL UNIQUE,
    payload_sha256  BYTEA NOT NULL,
    source          TEXT NOT NULL CHECK (source IN ('cli', 'sandbox')),
    media_type      TEXT NOT NULL DEFAULT 'application/json',
    payload         BYTEA,
    payload_s3_key  TEXT,
    workloads       INTEGER NOT NULL DEFAULT 0 CHECK (workloads >= 0),
    findings_json   JSONB NOT NULL DEFAULT '[]'::jsonb,
    view_count      INTEGER NOT NULL DEFAULT 0 CHECK (view_count >= 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    CONSTRAINT payload_xor_s3_key CHECK (
        (payload IS NOT NULL AND payload_s3_key IS NULL)
     OR (payload IS NULL     AND payload_s3_key IS NOT NULL)
    )
);

COMMENT ON COLUMN shared_analyses.payload_sha256 IS
    'Full 32-byte sha256. Distinct from hash (12-byte URL prefix) so an audit '
    'can collision-check the prefix space without re-reading payload bytes.';

COMMENT ON COLUMN shared_analyses.findings_json IS
    'Structured echo of detector findings. Lets leaderboard / pattern-library '
    'queries aggregate without unmarshalling payload.';

-- Postgres rejects non-immutable predicates in index expressions, so we
-- pin the lower bound to the epoch. The planner still uses the index for
-- any (expires_at > $1) probe.
CREATE INDEX shared_analyses_expires_at_idx
    ON shared_analyses (expires_at)
    WHERE expires_at > '1970-01-01'::timestamptz;

CREATE INDEX shared_analyses_source_created_idx
    ON shared_analyses (source, created_at DESC);

-- Explicit grant: the baseline's ALL TABLES grant only covers objects
-- created at baseline time, not in later migrations.
GRANT SELECT, INSERT, UPDATE ON shared_analyses TO optiqor_app;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS shared_analyses_source_created_idx;
DROP INDEX IF EXISTS shared_analyses_expires_at_idx;
DROP TABLE IF EXISTS shared_analyses;
-- +goose StatementEnd
