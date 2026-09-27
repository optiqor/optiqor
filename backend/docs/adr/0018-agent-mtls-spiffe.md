# 18. Agent identity is mTLS + SPIFFE URI SAN + 15-min HS256 JWT

Date: 2026-05-29
Status: Accepted
Supersedes: —

## Context

PR #31 introduced the in-cluster agent's egress path to the SaaS. The
agent needs a tenant identity proof that:

1. Cannot be spoofed by another cluster on the same network.
2. Cannot be replayed if a snapshot body is captured at rest.
3. Costs zero per request (no per-batch round-trip to an STS).
4. Builds without pulling a heavy framework into the Apache-2.0 agent.

The naive options were: bearer token (replay-friendly), AWS SigV4 (ties
the agent's identity to a cloud account), or a fresh OIDC exchange per
batch (one extra round-trip per snapshot — multiplies the egress cost
at the 60-second snapshot interval).

## Decision

Agent identity is the conjunction of:

- **Client mTLS** with a cert containing `URI: spiffe://optiqor.dev/tenant/<uuid>`
  in the SAN. The api binary's `MTLSTenantExtractor` parses the SVID
  and binds the tenancy context — no headers consulted.
- **HS256 JWT** in `X-Optiqor-Agent-Token` with a 15-minute TTL and the
  audience `agent-snapshot`. The token's `cluster_id` and `tenant_id`
  claims must match the snapshot body and the mTLS SVID respectively;
  any mismatch is a 401.
- **Bootstrap shared secret** ≥ 32 bytes, distributed via the install
  wizard, used to mint the JWT inside the agent and verify on the api.

The JWT proves recency (15-min replay window), not identity. The mTLS
SVID proves identity, not recency. Both fail closed.

## Why HS256 instead of Ed25519?

- The agent already ships the shared secret inside the cluster — no
  asymmetric key distribution problem to solve.
- HS256 is verifiable in one HMAC operation; Ed25519 is more expensive.
  We mint a fresh token every batch and verify on every snapshot;
  amortised CPU matters.
- The signed-token pattern in `internal/applyfix/token/` uses the same
  HS256 shape. Re-using the codebase's existing tested implementation
  is worth more than the marginal security argument for Ed25519.

## Why 15 minutes?

- Long enough that NTP skew across customer clusters doesn't bite.
- Short enough that a leaked snapshot body can't be replayed against
  next month's api binary.
- Anchored to the egress client's existing retry budget; a transient
  502 doesn't waste the token.

## Consequences

- Customers rotate the bootstrap secret on tenant offboarding. The
  install wizard generates one; the agent's Helm chart consumes it via
  `OPTIQOR_INGEST_SECRET` from a Kubernetes Secret.
- Adding a new agent endpoint (Phase 6+ Receipt verification) needs a
  new audience constant; reusing `agent-snapshot` would silently widen
  the existing token's blast radius.
- Cert rotation is the operator's concern via cert-manager. The
  egress client re-reads the cert pair on every TLS handshake, so a
  rolled cert takes effect at most one snapshot interval after the
  rotation completes.

## Alternatives considered

- **SPIRE / SPIFFE Workload API**: heavy infra dependency. Customers
  who already run SPIRE will get the URI SAN shape they expect; we
  don't force them to run SPIRE to onboard.
- **AWS IAM Roles Anywhere**: locks the agent to AWS. AKS and Hetzner
  ship in Year 1 per the ROADMAP.
- **OIDC exchange per batch**: 200-400 ms added latency per snapshot
  × 1440 snapshots/day = 5+ minutes of pure waiting per agent per
  day. Unacceptable for what's saved by getting "fresh" identity.
