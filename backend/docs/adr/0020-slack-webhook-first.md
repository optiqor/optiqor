# 20. Slack integration ships as incoming-webhook first; full OAuth deferred to Phase 7

Date: 2026-05-29
Status: Accepted

## Context

The ROADMAP committed Phase 5 to:
- Daily digest in Slack
- Weekly team report in Slack
- `/optiqor status` slash command

The full feature set requires a published Slack App with the
appropriate scopes, OAuth install flow, and an interactivity endpoint
the Slack platform calls back into. Slack App publishing requires
Slack platform review (typically 1-4 weeks for new apps, longer for
apps with bot scopes).

Design partner #1 was not going to wait 4 weeks for an integration
that's "table stakes" in their words.

## Decision

Phase 5 ships **Slack incoming webhook URLs** only:

- Tenant pastes a Slack incoming-webhook URL on the install wizard.
- Backend stores the URL KMS-encrypted in `slack_installations` (RLS
  scoped). The webhook URL is the only identity proof.
- Daily digest + weekly report fire as Temporal workflows, POST
  block-kit messages to the stored URL.
- Hostname is validated: URL must be on `hooks.slack.com` so a
  pasted-by-mistake URL can't ex-fil to an attacker domain.
- 5-second HTTP timeout, exponential retry on 5xx, no retry on 4xx.

Slash command + full OAuth install are tracked as Phase 7 work.

## Why webhook first?

- Customer can configure in 30 seconds (paste a URL).
- Customer maintains the channel + revocation; Optiqor doesn't have
  a bot user that needs offboarding.
- Slack platform approval is moot — webhooks don't require app
  publishing.
- The same Block Kit renderer used here will be re-used by the OAuth
  version, so the migration is "swap the transport, keep the body."

## Why defer the slash command?

- The slash command needs Slack's interactivity flow, which needs an
  authenticated app, which needs Slack-side approval. Capabilities,
  not effort.
- Daily digest + weekly report cover the "show me what Optiqor saved
  in chat" use case. The slash command is the "let me ask Optiqor
  ad-hoc" feature; it's net-positive but not blocking the design
  partner's go-live.

## Consequences

- `internal/notify/slack/` is webhook-only at Phase 5. Phase 7's OAuth
  swap touches: store schema (add `bot_token_ciphertext` column),
  poster (use `chat.postMessage`), new `/v1/slack/install` OAuth
  callback handler, new `/v1/slack/interact` interactivity handler.
- Existing webhook URLs continue to work post-Phase-7 — the OAuth
  swap is opt-in per tenant, not forced.
- `verify.sh` doesn't gate on slash-command presence (Phase 5 line
  was always "digest + report" specifically).

## Alternatives considered

- **Wait for full Slack OAuth approval**: blocks design partner #1 by
  3-4 weeks. Unacceptable.
- **Ship a self-hosted slash-command server with manual app
  installation**: customers would need to publish their own Slack app
  in their workspace; high friction.
- **Use Slack's legacy custom integrations**: deprecated path; Slack
  recommends webhooks for the simple posting case.
