# optiqor — Claude Conventions

The Optiqor proprietary monorepo. Go modular monolith producing three binaries from `cmd/`: `api`, `worker`, `agent`. Plus the Next.js web frontend (`web/`) and Terraform infrastructure (`infra/`).

Ground truth for stack and architecture is [docs/strategy/technical_implementation.md](docs/strategy/technical_implementation.md). When code disagrees with that doc, the doc wins unless an ADR in [docs/adr/](docs/adr/) records the change.

This file is the operating manual. Read it before writing or reviewing code.

---

## Stack

Boring tech, deliberately. We pick what we can debug at 3 AM.

- Go 1.24, single module (`github.com/optiqor/optiqor`)
- PostgreSQL 16 + TimescaleDB (multi-tenant via Row-Level Security)
- Redis 7 (cache, rate limits, LISTEN/NOTIFY when Postgres needs the assist)
- Temporal (workflow orchestration, per-tenant task queues)
- Anthropic Claude (Haiku enrichment, Sonnet generation, Opus on escalation only)
- AWS: EKS, RDS, ElastiCache, S3, KMS, Secrets Manager, CUR via Athena, STS
- Observability: Prometheus, Grafana, Loki, OpenTelemetry/Tempo, Sentry
- Web: Next.js 15 App Router, TypeScript strict, pnpm, Tailwind 4

If a problem reaches for Kafka, NATS, Neo4j, or a second language, the bar is an ADR that names the scaling or correctness wall you hit. Until then the answer is Postgres + Go.

---

## Architecture

### Layering

`internal/` holds domain packages. `internal/platform/` holds cross-cutting infrastructure.

Domain packages: `ingestion`, `parser`, `agent` (LLM orchestration), `cost`, `prwriter`, `receipts`, `rollback`, `confidence`, `sandbox`, `tenancy`, `gdpr`, `safety`, `onboarding`, `operators`, `billing`, `vcs`.

Cross-cutting: `platform/{config,db,logging,telemetry,featureflags,healthz}`.

Rules:

- Domain packages MUST NOT import each other directly. Cross-domain calls go through interfaces defined in the calling package. The callee never knows who calls it.
- Only `cmd/*` and `platform/*` instantiate concrete dependencies. Domain packages receive interfaces.
- `tenancy` is special. Every domain package's public method takes `*tenancy.Context` as the first arg after `ctx`. No exceptions.
- A new domain package needs an ADR if it pulls in a new external dependency or a new infra primitive.

### Multi-tenancy (non-negotiable)

Tenant isolation is a server-side guarantee, not an app-side one.

- Every Postgres query runs through a connection with `SET LOCAL app.tenant_id = '<uuid>'` set. RLS policies enforce isolation. No admin connection bypasses RLS in app code. Migrations use the `optiqor_migrator` role; everything else uses `optiqor_app`.
- Every Temporal workflow runs on a per-tenant task queue: `tenant-<uuid>-default`, `tenant-<uuid>-priority`. The queue name is the isolation primitive.
- Every Redis key is prefixed `t:<tenant_id>:`. Helpers in `platform/db/redis` enforce this. A direct `redis.Client` call outside the wrapper is a P0 bug.
- Every S3 path is prefixed `tenants/<tenant_id>/`. IAM policies scope further.
- Every log line carries `tenant_id`. The default `slog.Logger` injects it from context. A log line without `tenant_id` is an upstream bug.

The cross-tenant flag `is_superuser_context()` (migration 0003) exists for Leaderboard aggregations and pattern-library queries. Flipping it writes an `audit_log` row with a mandatory reason. Never flip without one. Never long-lived.

### Database

Migrations are forward-only and additive. Destructive changes (`DROP COLUMN`, type narrowing, `NOT NULL` on existing data, dropping an RLS policy) require an ADR with a backfill plan.

- Migration file naming: `NNNN_short_description.sql`. Goose-formatted, Up + Down blocks both required.
- Every tenant-scoped table gets an RLS policy in the same migration that creates it.
- Use `uuid_generate_v7()` (introduced in migration 0003) for append-heavy hot tables (time-ordered, index locality). `gen_random_uuid()` (v4) stays on existing baseline tables. Don't change historical IDs.
- `migrations/migrations_test.go` pins structural invariants for every migration. New migrations get new tests.
- Migration order is deterministic. Never reorder shipped migrations.
- The `app.tenant_id` session variable name is load-bearing. It must match `internal/platform/db.BindTenant`. Renaming on one side breaks every query on the other.

### Errors and panics

- Wrap with `%w`, never `%s`, when the caller might use `errors.Is` / `errors.As`.
- Sentinel errors are exported only when callers must branch on them: `var ErrNotFound = errors.New(...)`. Otherwise hide behind a method.
- Never panic in a handler. The `withPanicRecovery` middleware exists as a safety net, not a contract. Panics are logged, captured to Sentry with tenant context (with secrets redacted), and returned as 500.
- `errors.New("constant")` over `fmt.Errorf` when there's no variable.
- Distinguish expected errors (input bad, resource gone) from unexpected (RPC down, DB unreachable). Expected returns 4xx; unexpected returns 5xx and a Sentry capture.

### Concurrency

Preference order, top wins:

1. No concurrency. Serial code is correct by default.
2. Goroutine + bounded channel for fan-out work.
3. `sync.WaitGroup` for goroutine lifecycle.
4. `sync.Mutex` only when shared state can't be designed away.
5. `atomic` for single-word counters where mutex overhead matters.

Every goroutine you spawn must have a clear exit. If you can't draw the lifecycle on a napkin, don't spawn it.

`context.Context` is always the first arg. Never store context in a struct, except as the cancellation seed for a long-running goroutine, and document why in a comment.

### Time and randomness

Time and randomness are dependencies. Inject them.

```go
type Clock interface { Now() time.Time }

// In tests:
fakeClock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
```

Never call `time.Now()` directly in any package with a state machine, timeout, or expiry. Never use `math/rand` global state. Accept a `*rand.Rand` or `io.Reader`.

`crypto/rand` for anything security-sensitive (signing nonces, token generation, key material). `math/rand` for sampling and jitter only.

### HTTP handlers

Every public handler does, in order:

1. Method check. Wrong method returns 405 fast.
2. Body size cap via `http.MaxBytesReader`. Caps live in `internal/platform/config/httpcaps.go`. No bare literals at the call site.
3. Tenant resolution. Header (Phase 1 dev) or JWT (Phase 5+). If tenant context isn't set, fail closed with 400.
4. JSON decode with `DisallowUnknownFields`. Reject typos at the door.
5. Business logic returning `(result, error)`.
6. Single error-rendering helper translates errors to status codes. Handlers don't write to `ResponseWriter` directly past the helper.

Handlers don't log. They call domain methods that log with `tenant_id` injected. Handlers don't allocate `context.Context` either; they propagate `r.Context()`.

### Observability

- Structured logs via `slog`. Always `tenant_id`, `request_id`, `workflow_id` when in scope. The handler-side helpers in `platform/logging` enforce this.
- Metrics via the Prometheus client. Naming: `optiqor_<domain>_<metric>_<unit>`. Histograms for latency, gauges for instantaneous state, counters for events. Latency histograms use the standard bucket set in `cmd/api/main.go`.
- Traces via OpenTelemetry to Tempo. Every Temporal activity is its own span. Every HTTP handler is its own span.
- Errors to Sentry with tenant context. Never customer secrets, AWS keys, GitHub App private-key fragments, or LLM prompt/response bodies. Redaction lives in `internal/platform/telemetry`.

A piece of information goes to logs, metrics, or traces. Pick one, not all three. Logs are narrative, metrics are aggregates, traces are causality.

### Secrets

- Never commit real secrets. `.env` is gitignored. `.env.example` ships.
- AWS credentials: STS AssumeRole. No long-lived access keys.
- GitHub App private key: AWS Secrets Manager, fetched at startup.
- Anthropic API key: AWS Secrets Manager.
- Customer secrets (their AWS keys, GitHub tokens): KMS-encrypted at rest, decrypted only inside Temporal workflow context, never in API response paths.
- Config validation enforces prod-required secrets in `internal/platform/config/config.go`. If you add a secret, add it to `Validate()` in the same commit.
- CI runs `gitleaks` on every PR. CodeQL is intentionally not wired (private repo, no GHAS seat). We rely on `gosec` + `govulncheck` + `trivy` for SAST.

---

## Testing

- Unit tests next to the code (`foo.go` + `foo_test.go`), same package.
- Integration tests in `tests/integration/` use `testcontainers` for real Postgres + Redis. Never mock the database.
- E2E in `tests/e2e/` spins up the full stack via `docker-compose.test.yml`.
- Race detector on in CI. Always.
- Coverage target: 70% on domain packages, 50% on `platform/`. Higher when the code path is hot or load-bearing.

Test naming: `TestFunctionName_Scenario_ExpectedBehavior`. Read aloud: "Test ParseValues, empty file, returns ErrEmpty".

Table tests for any branch count >2. Use subtests so a failure points at the row, not the harness.

Golden tests for renderer output and any deterministic byte-stream. Regenerate with `-update`. Eyeball the diff before committing. A golden change without a code change is the canary for an accidental output drift.

---

## LLM usage

- Sonnet for diff generation. Haiku for enrichment, classification, simple structured extraction. Opus only on escalation (<5% of calls).
- Anthropic prompt caching is mandatory. Structure prompts so the system prompt + pattern library is the cached prefix. Target: 50% cache hit rate Year 1, 75% Year 2.
- Cost cap per analysis: $0.40. Enforced in `internal/agent/budget.go`. Every LLM call site consults the cap.
- Every LLM call records cost in `llm_calls` for attribution.
- Defense in depth: sanitise input through `internal/agent/llm/sanitizer`, use structured prompts, run a second-pass validator on outputs, run deterministic post-validators on generated diffs (no syntax errors, no removed labels, no out-of-bounds resource changes).

---

## Performance

- p99 latency budgets are documented per-endpoint in `docs/api.md`. Don't ship code that misses the budget. If the budget moves, write an ADR.
- Allocation discipline: anything in the per-request hot path doesn't allocate. Use `sync.Pool` for buffer reuse. Prefer indexing over `append` in tight loops.
- Database: every query has an index, verified via `EXPLAIN` for anything beyond a primary-key lookup. `pg_stat_statements` is on in dev; read it after meaningful schema changes.
- One round-trip per request when possible. N+1 is a bug, not a perf concern.

---

## Code style

### Naming

- Functions are verbs (`OpenChart`, `RenderReport`). Methods on a noun receiver: `chart.Render()`.
- Interfaces with one method end in `-er` (`Reader`, `Renderer`). Multi-method interfaces are named after the role (`Store`, `Dispatcher`).
- Predicates: `isLeader`, `hasQuorum`, not `leaderFlag`.
- Plurals are sets, singulars are individuals: `workloads []Workload`, `workload Workload`.
- Acronyms keep case: `httpClient`, `parseURL`, `IDToken`. Not `HttpClient`, `parseUrl`.

### Functions

- One thing per function. If the name has "and" in it, split.
- Early return for guard clauses. No `else` after `return`.
- Receiver name is one or two letters and consistent: every method on `Composer` uses `c *Composer`.
- `~50` lines is a smell. `~100` is a bug. Long functions hide intent.

### Comments

Comments explain why, never what. Code says what; you say why it has to.

**Bad (states the obvious, AI-shaped):**

```go
// Iterates through workloads and processes each one.
for _, w := range workloads {
    process(w)
}
```

**Good (explains the why):**

```go
// Sequential on purpose. The detector pipeline mutates a shared score
// map that's faster to merge than to lock per-write. ADR-0003.
for _, w := range workloads {
    process(w)
}
```

**Bad (AI godoc that says nothing):**

```go
// MonthlyUSDCents calculates the monthly cost in USD cents.
// Returns 0 if replicas is 0.
func MonthlyUSDCents(w Workload, p Prices) int64
```

**Good (godoc that earns its keep):**

```go
// MonthlyUSDCents projects a steady-state monthly cost. Replicas are
// clamped to >= 1 because Kubernetes Deployments with replicas=0 still
// occupy the scheduler's bookkeeping and are re-evaluated each cycle.
func MonthlyUSDCents(w Workload, p Prices) int64
```

Reference real things: issue numbers, commit SHAs, ADR numbers, RFC sections, vendor docs. Specific over abstract.

```go
// k8s 1.31 narrowed the projected-token audience claim. Older clusters
// still expect "https://kubernetes.default.svc". See ADR-0007.
```

**Banned in comments:**

- Markdown headers (`#`, `##`, `###`).
- `Note:`, `Important:`, `Caution:`, `Warning:` labels. If it's important, the code structure should make the rule unmissable.
- Emojis. None, anywhere.
- Restating what the code says.
- "Helper function to...", "Utility for...". Describe what it does in the name.
- Author tags (`// jdoe, 2024-05-12`). Git blame exists.
- TODOs without a name or issue. `// TODO(@shivam, #42): ...` is fine. `// TODO: do later` is not.

Godoc is for the package and exported symbols. Every exported symbol gets one sentence minimum. The first sentence starts with the symbol name. Examples for non-trivial APIs go in `example_test.go`.

### Files and packages

- One file per concept up to ~500 lines. Past that, split by behaviour, not size.
- File names are lowercase, no underscores except `_test.go` and `_test_helpers.go`.
- `doc.go` per package, package comment lives there.
- Package names are short, single-word, no plurals, no `util`, no `common`, no `helpers`.

### Dependencies

- A new transitive dependency needs a one-line justification in the commit message and a comment in `go.mod` next to the `require`.
- Prefer stdlib. The Go stdlib is well-tested and updates on our cadence via the Go version bump.
- Permissive licences only (Apache 2.0, MIT, BSD, MPL 2.0). No GPL or LGPL.
- Versions are pinned. Renovate / Dependabot lifts them; humans approve.

### Public surfaces

This repo has no public Go API surface. Everything is `internal/`. The public surface is:

- HTTP API (`docs/api.md`)
- Database schema (`migrations/`)
- Temporal workflow signatures (`internal/worker/workflows/`)
- The CLI's `pkg/` (imported from `github.com/optiqor/optiqor-cli` via go.mod replace)

Breaking changes to any of those require an ADR and a versioning plan (e.g. `/v2/...` for HTTP, deprecation window for workflows).

---

## Workflow

### Branches

- `<type>/<short-kebab-slug>`. Same prefixes as commits: `feat/`, `fix/`, `chore/`, `docs/`, `test/`, `refactor/`.
- Branch off `main`. Never branch off a feature branch.
- Delete merged branches.

### Commits

Conventional Commits. One concern per commit. DCO sign-off required (`-s` flag).

See [.claude/skills/commit/SKILL.md](.claude/skills/commit/SKILL.md) for the rules and the local quality gate (gofmt + vet + build + test -race + lint).

### Pull requests

- Open against `main`.
- Title is a single Conventional Commit subject; it becomes the squash-merge subject.
- Body tells the reviewer the story: what changed, why, how to verify. Not a diff narration.
- Link the issue: `Closes #N`.
- Self-review the diff before requesting one. Read it as a stranger would.

### Reviews

See [.claude/skills/pr-review/SKILL.md](.claude/skills/pr-review/SKILL.md) for the voice and the line-anchoring + verdict rules.

### Decisions

Non-trivial architectural changes get an ADR in `docs/adr/NNNN-title.md`. Template at `docs/adr/0000-template.md`. ADRs are short, opinionated, dated.

Examples of "non-trivial":

- A new domain package
- A change to the tenancy or RLS model
- A new external dependency category (message queue, search index)
- Migration ordering change
- A change to a public HTTP route shape

---

## Anti-patterns ("don't")

### Architecture

- Don't add a microservice. Modular monolith until team >= 15 or a real scaling wall hits.
- Don't reach for Kafka / NATS. Postgres LISTEN/NOTIFY in Year 1.
- Don't reach for Neo4j. Recursive CTEs cover our graph needs.
- Don't introduce a second language. Go everywhere. Python is allowed only in a dedicated `ml-training` repo, invoked offline.
- Don't ship Apply Fix for StatefulSets in Year 1. The blast radius is too wide.

### Code

- Don't write a singleton. Pass dependencies through the constructor.
- Don't have package-level mutable state.
- Don't depend on `init()` order. If `init()` does anything beyond registering handlers, it's a smell.
- Don't `panic` for control flow. Reserved for "this can never happen".
- Don't return interface types when a concrete type would do. The caller picks the abstraction.
- Don't accept `interface{}` / `any` unless you've considered generics first.
- Don't use named returns to "save typing". They're for documenting non-obvious return semantics.

### Process

- Don't merge to `main` without a PR. Admin bypass on branch protection exists for emergency; an emergency push needs an issue explaining why a PR couldn't wait.
- Don't squash-merge before CI is green.
- Don't import the CLI's `internal/`. Only `pkg/` is the public surface. The CLI stays independently auditable as Apache 2.0 OSS.
- Don't commit binary artifacts. Generated files go in `.gitignore`.
- Don't write a TODO without an issue. Open the issue, paste the link, then write the TODO.
- Don't run migrations against prod without a dry-run plan and a rollback path.
