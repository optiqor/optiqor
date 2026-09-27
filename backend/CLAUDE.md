# optiqor — Claude Conventions

The Optiqor backend, living in `backend/` of the `optiqor-cli` repo as its own Go module. Go modular monolith producing three binaries from `cmd/`: `api`, `worker`, `agent`. Plus the Next.js web frontend (`web/`) and Terraform infrastructure (`infra/`).

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

Comments explain WHY, never what. Code says what; you say why it has to.

**Decoration is debt.** Every comment is a thing a future engineer must keep in sync with the code. Most comments don't earn their keep — they restate the signature, narrate the obvious, or pad a function's preamble with "philosophy" prose. When in doubt, delete. Re-add only when a reader would otherwise miss a non-obvious constraint, trade-off, ADR reference, security/performance rationale, or invariant.

#### Godoc on exported symbols

The Go convention says every exported symbol gets a godoc starting with its name. We follow this **only when the godoc adds value beyond the symbol name + signature**. A `func NewSigner(secret []byte) *Signer` does not need `// NewSigner returns a Signer.` — the name says it. A `func NewSigner(secret []byte) *Signer { if len(secret) == 0 { panic(...) }; ... }` does benefit from `// NewSigner panics on an empty secret so a misconfigured boot fails closed instead of issuing tokens nobody can verify.` because the panic-on-empty contract is non-obvious from the signature.

Rule of thumb: if removing the godoc costs the reader nothing, the godoc costs the codebase nothing to delete. Audit each one.

#### Bad / good examples

**Bad — restates the signature:**

```go
// Mount registers GET /v1/session/whoami and POST /v1/session/issue.
func (h *Handler) Mount(mux *http.ServeMux) {
    mux.HandleFunc("GET /v1/session/whoami", h.Whoami)
    mux.HandleFunc("POST /v1/session/issue", h.Issue)
}
```

**Good — drop the godoc; the method body already enumerates the routes.**

---

**Bad — multi-paragraph "philosophy" package docstring:**

```go
// Package agent is the SaaS-side LLM orchestrator that turns a
// [rules.Finding] into a human-readable explanation and a unified
// `values.yaml` diff suggesting the fix.
//
// Phase 1 contract:
//
//   - Inputs are sanitised via internal/agent/llm/sanitizer before
//     leaving the boundary. Customer secrets, file paths, and prompt
//     injection markers are stripped or wrapped.
//   - The LLM call goes through an [LLMClient] interface so:
//   - tests run against a deterministic [FakeLLMClient];
//   - the real Anthropic SDK adapter ships behind an env flag
//     without forcing every test path to depend on it.
//   ...
package agent
```

**Good — same facts, one paragraph, load-bearing context only:**

```go
// Package agent turns a rules.Finding into an explanation + unified
// values.yaml diff via an LLM. Sanitises input through
// internal/agent/llm/sanitizer; enforces the per-call $0.40 cap before
// any network egress; records every call against the llm_calls table.
package agent
```

---

**Bad — narrates what the code says:**

```go
// Iterates through workloads and processes each one.
for _, w := range workloads {
    process(w)
}
```

**Good — explains the non-obvious WHY:**

```go
// Sequential on purpose. The detector pipeline mutates a shared score
// map that's faster to merge than to lock per-write. ADR-0003.
for _, w := range workloads {
    process(w)
}
```

---

**Bad — bullet-listed enumeration that duplicates struct field tags:**

```go
// PreviewResponse echoes the rendered Markdown body, the unified diff
// suitable for git apply, the sanitizer's verdict on the input chart,
// and a generated explanation string.
//
//   - MarkdownBody: the PR-comment markdown
//   - UnifiedDiff:  the diff against the original chart
//   - Explanation:  the LLM's narrative
//   - SanitizerApplied: true if the sanitizer modified the input
type PreviewResponse struct {
    MarkdownBody     string `json:"markdown_body"`
    UnifiedDiff      string `json:"unified_diff"`
    Explanation      string `json:"explanation"`
    SanitizerApplied bool   `json:"sanitizer_applied"`
}
```

**Good — drop the godoc; the struct + json tags already document the wire shape.**

---

**Good — references real things (issue numbers, ADR numbers, RFC sections, vendor docs):**

```go
// k8s 1.31 narrowed the projected-token audience claim. Older clusters
// still expect "https://kubernetes.default.svc". See ADR-0007.
```

```go
// Constant-time compare avoids leaking the token byte-by-byte via
// timing. crypto/subtle.ConstantTimeCompare is the only safe path.
```

```go
// Headers per RFC 9110 §10.2.3 — Retry-After in seconds, minimum 1.
```

#### Banned in comments

- Markdown headers (`#`, `##`, `###`) inside `//` comments. Go and TS readers don't render markdown in source.
- `Note:`, `Important:`, `Caution:`, `Warning:` labels. If it's important, the code structure should make the rule unmissable.
- Em-dash-heavy narrative essays in package or function docstrings. One terse sentence beats five lines of glue.
- Decorative section dividers in code: `// ─── Helpers ───`, `// ====== validators ======`. Use blank lines.
- Multi-paragraph package docstrings narrating "Layout philosophy:", "Implementation notes:", "This file is the operational mirror of...". Compress to one or two terse sentences capturing the load-bearing facts.
- Bullet-listed enumerations of struct fields or function returns — the type tags already enumerate them.
- Restating what the next line of code says.
- Restating the signature: `// Foo returns a Foo.`, `// Bar bars the baz.`
- "Helper function to...", "Utility for...", "This function..." preambles. Describe the WHY or delete.
- Emojis. None, anywhere.
- Author tags (`// jdoe, 2024-05-12`). Git blame exists.
- TODOs without a name and issue: `// TODO(@shivam, #42): ...` is fine; `// TODO: do later` is not.

#### How to audit a comment

Ask one question: *"if I delete this, what does the next engineer fail to understand?"* If the answer is "nothing — the name + signature + types already say it", delete. If the answer names a specific constraint, trade-off, ADR, workaround, security/perf concern, or invariant, keep but compress to its tightest form.

Errors in error messages follow the same rule — no em-dashes for clause-glue, use commas or rewrite. `errors.New("auth: invalid token")` is right; `errors.New("auth: invalid token — verify failed against current secret")` is not (the second clause is restating, not adding).

#### Reference commits

The 2026-05-24 cleanup compressed the codebase by ~1,500 lines of comment cruft across four phases (commits `cdec3a3`, `723e409`, `9de1674` on the backend; `feba8e0`, `9f817cd` on the CLI). Read any of those diffs to see the tone applied at scale; new code should land at that compression level from the start, not need a follow-up sweep.

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
- The CLI's `pkg/` (`github.com/optiqor/optiqor-cli`, the repo root module, wired via `replace => ../`)

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
