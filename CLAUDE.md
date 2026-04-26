# backend — Claude Conventions

This is the Costify proprietary monorepo: Go modular monolith producing three binaries (`api`, `worker`, `agent`) from `cmd/`. Ground truth for stack and architecture decisions is [docs/strategy/technical_implementation.md](docs/strategy/technical_implementation.md). When code disagrees with that doc, the doc wins unless an ADR in [docs/adr/](docs/adr/) records the change.

## Stack

- Go 1.23+, single module
- PostgreSQL 16 + TimescaleDB extension (multi-tenant via Row-Level Security)
- Redis 7 (cache, rate limiting)
- Temporal (workflow orchestration; per-tenant task queues)
- Anthropic Claude (Haiku for enrichment, Sonnet for generation, Opus on escalation only)
- AWS: EKS, RDS, ElastiCache, S3, KMS, Secrets Manager, CUR via Athena, STS AssumeRole
- Observability: Prometheus + Grafana + Loki + OpenTelemetry/Tempo + Sentry

## Layering rules (`internal/`)

Domain packages: `ingestion`, `parser`, `agent` (LLM orchestration), `cost`, `prwriter`, `receipts`, `rollback`, `confidence`, `sandbox`, `tenancy`. Cross-cutting: `platform/{config,db,logging,telemetry,featureflags}`.

- Domain packages **must not** import each other directly. Cross-domain calls go through interfaces defined in the calling package.
- Only `cmd/*` and `platform/*` may instantiate concrete dependencies. Domain packages take interfaces.
- `tenancy` is special: every domain package's public API takes a `*tenancy.Context` as the first arg after `ctx`.

## Multi-tenancy is non-negotiable

- **Every Postgres query** runs through a connection that has `SET LOCAL app.tenant_id = '<uuid>'` set. RLS policies enforce isolation server-side. There is no escape hatch — no admin connection bypasses RLS in app code. (Migrations use a separate role.)
- **Every Temporal workflow** runs on a per-tenant task queue: `tenant-<uuid>-default`, `tenant-<uuid>-priority`.
- **Every Redis key** is prefixed `t:<tenant_id>:`. Helpers in `platform/db/redis` enforce this.
- **Every S3 path** is prefixed `tenants/<tenant_id>/`. IAM policies scope further.
- **Every log line** carries `tenant_id`. The default `slog.Logger` injects it from context.

## Secrets

- **Never** commit real secrets. `.env` is gitignored; only `.env.example` ships.
- AWS credentials: STS AssumeRole, never long-lived keys.
- GitHub App private key: stored in AWS Secrets Manager, fetched at startup.
- Anthropic API key: AWS Secrets Manager.
- Customer secrets (their AWS credentials, GitHub tokens): KMS-encrypted at rest, decrypted only inside Temporal workflow context.
- CI: `gitleaks` runs on every PR.

CodeQL is intentionally **not** wired into this private repo — GitHub Advanced Security is required for CodeQL on private repos and we have not bought the seat. We rely on `gosec` + `govulncheck` + `trivy` (filesystem) for SAST; see `.github/workflows/security.yml`. If GHAS is later enabled at the org level, restore `.github/workflows/codeql.yml` from the cli repo's identical workflow.

## Testing

- Unit tests beside the code (`foo.go` + `foo_test.go`).
- Integration tests in `tests/integration/` use `testcontainers` for real Postgres + Redis. **Do not mock the database.**
- E2E in `tests/e2e/` spins up the full stack via `docker-compose.test.yml`.
- Race detector on in CI (`go test -race`).
- Coverage target: 70% on domain packages, 50% on `platform/`.

## LLM usage

- Default to Sonnet for diff generation. Haiku for enrichment / classification. Opus only on escalation (target <5% of calls).
- Anthropic prompt caching is mandatory: structure prompts so the system prompt + pattern library is the cached prefix. Target cache hit rate: 50% Year 1, 75% Year 2.
- Cost cap per analysis: $0.40. The cost gate lives in `internal/agent/budget.go`.
- Every LLM call records its cost in `llm_calls` for attribution.

## Observability

- Structured logs via `slog` with `tenant_id`, `request_id`, `workflow_id` always present.
- Metrics via Prometheus client. Naming: `costify_<domain>_<metric>_<unit>`. Histograms for latency.
- Traces via OpenTelemetry → Tempo. Every Temporal activity is its own span.
- Errors → Sentry with tenant context (but **never** customer secrets in Sentry payloads — redaction is mandatory).

## Don't

- Don't add a microservice. We're a modular monolith until team ≥ 15 or a real scaling wall hits.
- Don't reach for Kafka/NATS — use Postgres LISTEN/NOTIFY in Year 1.
- Don't reach for Neo4j — Postgres recursive CTEs handle our graph needs.
- Don't introduce a second language. Go everywhere. Python only enters the stack in Year 2 if ML training requires it (and only in an isolated `ml-training` service invoked offline).
- Don't ship Apply Fix for StatefulSets in Year 1 — too risky.
- Don't import `cli/` code. The CLI must stay independently auditable as Apache 2.0 OSS.
