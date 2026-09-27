-- +goose Up
-- +goose StatementBegin
--
-- tenants.skeptic_mode_default — the safest-by-default posture for
-- design partner #1. New tenants default to true; existing rows stay
-- on false so behaviour for pre-launch testers does not change
-- silently. ApplyFix workflow reads this flag at dispatch.
--
-- Phase 5 commitment: "Skeptic Mode default-on for new customers".

ALTER TABLE tenants
    ADD COLUMN skeptic_mode_default BOOLEAN NOT NULL DEFAULT true;

-- Existing rows pre-date the flag; flip them off so we don't change
-- behaviour for any pre-launch testers in flight. The default true
-- only kicks in for tenants inserted after this migration.
UPDATE tenants SET skeptic_mode_default = false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tenants DROP COLUMN IF EXISTS skeptic_mode_default;
-- +goose StatementEnd
