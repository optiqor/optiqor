# backend — Sprint Todo

Backend-scoped subset of the org-level [ROADMAP.md](ROADMAP.md). This file is the **canonical engineering tracker for backend work**; CLI-side phase work lives in the [optiqor repo](https://github.com/optiqor/optiqor-cli). The cross-repo Phase view (cost-detector breakdowns, CLI runtime status, etc.) lives in [ROADMAP.md](ROADMAP.md) — keep both files in sync when a phase milestone moves.

> **Today: 2026-05-24.** Active phase: **Phase 2 — Public Sandbox (Weeks 3–4).** Phase 1 closed 2026-05-11 and re-verified 2026-05-24. **Phase 2 closed 2026-05-24** with the full vertical:
> - Backend: migration 0004 + PgStore (shared analyses), `internal/platform/ratelimit` (memory limiter wired into sandbox routes), p95 < 3s benchmark in CI, `internal/auth` session JWT signer + `/v1/session/whoami` + `/v1/session/issue` handlers, `internal/onboarding` HTTP wrappers + state machine `/v1/onboarding/state` + `/v1/onboarding/transition`.
> - Frontend: Auth.js v5 (GitHub provider) + middleware-gated `/app/*`, dashboard shell (`/app`, `/app/analyses`, sidebar nav), sign-in page (`/signin`), onboarding wizard on `/install` that drives the backend state machine, typed API client extended with whoami/onboarding helpers.
> - Spec: `optiqor-cli/docs/api/openapi.yaml` (full public surface, 17 paths) + `scripts/check-openapi-parity.sh` CI gate; ADR-0016 frontend stack.
> - Gates: `go vet ./...` clean · `go test -race ./...` **32 pkgs** clean · `./verify.sh` → **125 PASS · 0 FAIL · 4 GAP** (same Phase-5 gaps) · golangci-lint clean · OpenAPI parity 17/17 · `next build` clean (16 routes) · ESLint clean.
> - Remaining Phase-2 work: S3 store adapter for >256 KiB payload promotion (single-file swap once AWS binds), Vercel preview deployment config.
>
> **Year 1 surface (expanded):** AWS EKS · Azure AKS · Hetzner Cloud K8s · GitHub · GitLab · ArgoCD · Flux CD · Helm · Kustomize. Day 90 demo stays narrow (EKS + GitHub + ArgoCD + Helm); the rest lands in Phases 7-9 (Months 4-12).
>
> **Cross-repo split (consistent with [ROADMAP.md](ROADMAP.md)):**
> - **This repo (backend)** — proprietary monorepo: API server, Temporal worker, in-cluster K8s agent, sandbox web frontend, Terraform infra, Receipt issuer, LLM Apply Fix path
> - **[optiqor/optiqor-cli](https://github.com/optiqor/optiqor-cli)** — Apache-2.0 OSS CLI: deterministic 30-detector rule engine, `analyze`/`demo`/`diff`/`score`/`audit`/`compare`, `--share` HTTPS upload, `@optiqor/cli` npm package

## Open strategic decisions (write before unblocking the dependent phase)

These are decisions, not engineering work. They block phases as listed; without an explicit call, the engineering team will make the call implicitly and the wrong way.

- [ ] **Day-90 scope: single-cloud-ready vs multi-cloud-ready** (blocks Phase 5 hiring plan + seed-round size). README says EKS-only Day 90 with AKS/GitLab/Hetzner as Phases 7-9. Year-1 surface expansion (2026-04-26 amendments) means the codebase has stub adapters for those clouds today. Decision: are we shipping (a) EKS-only Day 90 with abstractions ready but dormant — current 3-engineer team works, OR (b) EKS + AKS parity by Day 90 — requires 5 engineers by Month 3 + larger seed. **Decide explicitly + tell the board.** Decision drives Month-3 hiring + seed-round target.
- [ ] **ADR-0016 — Recommendation engine scope-and-sequencing for Year 1** (blocks Phase 6 plan finalization). Phase 6's 2-week box treats the recommendation engine as one line item; the actual math is 4-6 engineer-months when written conservatively. Three options laid out in the Phase 6 §scoping reality block below: (1) cut to `hybrid_v0` and ship in 2 wk; (2) extend Phase 6 to 8-12 wk; (3) compress at lower quality. Recommendation: option (1). Land the ADR before Phase 6 starts.
- [x] **ADR-0017 — Receipt signing algorithm: ECDSA P-256 (KMS-native)** — accepted 2026-05-24 (PR #20, `docs/adr/0017-receipt-signing-algorithm.md`). Supersedes ADR-0011 §"Key algorithm" only; the rest of ADR-0011 (KMS-only, per-region keys, CloudTrail-logged signing) stands. No third-party verifier tooling has shipped yet so the cost of switching is roughly zero now and high later.
- [ ] **`optiqor.dev/works-with/kubecost` Month-4 GTM page** (commitment in `business_strategy.md §6.3`). Two sections: (1) How they're different — Kubecost is the dashboard, Optiqor is the PR-time loop-closer; (2) Use both together — keep Kubecost dashboard, install Optiqor's GitHub App. **Hold publication until ≥1 verified Cloud Receipt exists** (Phase 6 exit) so the page leads with proof, not claims. Owner: founders / DevRel; not engineering work. Decision needed: who drafts the page and approves it.
- [ ] **DR drill cadence** (per ADR-0008 + tech_impl §3.5). Monthly automated restore drills via Temporal cron; oncall paged on failure. Currently in Phase 6 todo list as a single item. **Decide:** does drill #1 happen at Phase 6 exit (passive) or Phase 6 entry (forcing function to find issues early)? Lean entry. Decision lands when the cron is wired.

## Phase 1 — Weeks 1–2: Foundation

> **Status (2026-05-11):** Phase 1 **CLOSED — production-ready code + infra-as-code surface complete, zero open code gaps.** The remaining `[ ]` items below all require an AWS account and live infrastructure (`terraform apply`, EKS bootstrap, ArgoCD install); they ship in the first sprint after pre-seed funding binds the AWS account. The Terraform code itself is committed and `terraform fmt -check`-clean (wired into `make lint` + CI).
>
> **Production-readiness evidence (`./verify.sh`):**
> - **125 PASS · 0 FAIL · 4 GAP** — every remaining gap is explicitly scheduled Phase 5 (Sentry SDK wire, agent's K8s informer / Prometheus scrape / mTLS) and requires an external system to validate, not new code.
> - `go test ./... -race` clean across all 30 packages; `go vet ./...` clean.
> - ~6,500 LOC production + ~5,100 LOC tests (78% test-to-code ratio).
> - Every HTTP route has middleware (panic recovery, request-id, structured access log) and a body-size cap; OAuth callback validates state; GitHub webhook verifies HMAC; pprof gated by constant-time token.
> - RLS enforced on every tenant-scoped table; `optiqor_app` / `optiqor_migrator` role split in baseline migration; tenant context required by every domain entrypoint.
> - All three container images run as non-root from a distroless base; `gitleaks` clean; no committed `.env` / `*.pem` / `*.key`.

### Infra (Terraform — code complete, apply pending AWS account)
- [x] `infra/terraform/modules/vpc` — VPC, subnets (public/private/db), NAT, IGW, flow logs
- [x] `infra/terraform/modules/eks` — EKS 1.31 cluster, managed node groups, OIDC/IRSA, secrets-envelope encryption
- [x] `infra/terraform/modules/rds-postgres` — RDS Postgres 16, `pg_stat_statements` parameter group, automated backups (PITR 35d), KMS encryption, performance insights, multi-AZ-by-default
- [x] `infra/terraform/modules/elasticache` — Redis 7.1, encryption at-rest + in-transit, AUTH token in Secrets Manager
- [x] `infra/terraform/modules/s3` — KMS-encrypted bucket with versioning, public-access block, lifecycle, optional Cross-Region Replication
- [x] `infra/terraform/modules/kms` — multi-region symmetric key for at-rest, multi-region asymmetric `SIGN_VERIFY` key for Receipt signing
- [x] `infra/terraform/modules/iam` — GitHub OIDC provider, ECR push role, ArgoCD IRSA role
- [x] `infra/terraform/envs/dev/main.tf` — wires modules, single-AZ, low-tier classes
- [x] `infra/terraform/envs/staging/main.tf` — multi-AZ, prod-twin sized smaller
- [x] `infra/terraform/envs/prod/main.tf` — us-east-1 multi-AZ, multi-region KMS, S3 CRR enabled, deletion protection

### App platform (`internal/platform/`)
- [x] `config` — typed env loader, prod-required-secrets validation, duration parsing with fallback
- [x] `db` — Postgres tenant bind helpers (`set_config('app.tenant_id', $1, true)`) and Redis Keyspace with `t:<tenant>:` prefix enforcement
- [x] `logging` — slog handler that injects `tenant_id`/`workspace_id`/`cluster_id`/`namespace`/`request_id`/`workflow_id` from context
- [x] `healthz` — readiness check registry with per-check timeouts
- [x] `telemetry` — Prometheus registry + OTel tracer/meter scaffolding (no-op tracer until OTel collector lands)
- [x] `featureflags` — OpenFeature client wrapper with no-op default; bool/string/number flag helpers

### Cross-cutting abstractions (Year 1 expansion enablers)
- [x] `internal/billing/` — pluggable `Source` registry; AWS CUR + Capacity tier stubs (Phase 6+ wires Athena query path)
- [x] `internal/vcs/` — pluggable `Source` registry; GitHub HMAC-SHA256 webhook verification ready for Phase 4
- [x] `internal/parser/gitops/` — ArgoCD `Application` reader that emits a normalised representation; Flux land in Phase 7

### `cmd/api`
- [x] HTTP server (stdlib mux), graceful shutdown via `signal.NotifyContext` + configured grace period
- [x] `/healthz` (liveness), `/readyz` (readiness — registry-driven 200/503 + JSON results), `/metrics` (Prometheus exposition)
- [x] Middleware: request_id, tenant resolution from `X-Optiqor-Tenant` header (Phase 1 dev surface), slog access log, panic recovery
- [x] GitHub App webhook receiver — HMAC verification + 8MiB body cap + 202 ack
- [x] GitHub OAuth callback handler (Phase 1 ack stub — session issuance lands in Phase 5)
- [x] pprof endpoints behind `OPTIQOR_ADMIN_TOKEN` constant-time check (disabled when token is empty)
- [ ] JWT-based tenant resolution (Phase 5 — replaces header extractor when auth ships)

### `cmd/worker`
- [x] Worker boot scaffolding (config, logger, signal-driven shutdown)
- [x] Temporal client connection stub + per-tenant queue dispatcher (real workflow registration lands in Phase 3)
- [x] First end-to-end placeholder workflow — `internal/worker/workflows/echo` registered at boot

### `cmd/agent`
- [x] Stub binary that prints version + idle loop until SIGTERM. Real watch loop ships Phase 5.

### CI/CD
- [x] `.github/workflows/ci.yml` — golangci-lint + `go test -race ./...`
- [x] `.github/workflows/security.yml` — gosec + govulncheck + trivy + gitleaks
- [x] `.github/workflows/release.yml` — build → ECR (cosign-signed) → ArgoCD sync trigger _(stub; needs AWS account binding)_
- [x] `.github/workflows/codeql.yml` _(skipped at runtime — private repo without GHAS)_
- [x] `.github/dependabot.yml`
- [x] `.github/CODEOWNERS`

### Migrations
- [x] goose-format `migrations/0001_baseline.sql` with: `tenants`, `workspaces`, `clusters`, `namespaces`, `workloads`, `recommendations`, `recommendation_dismissals`, `apply_fixes`, `receipts`, `llm_calls`, `audit_log`
- [x] RLS policies on all tenant-scoped tables (`tenant_isolation USING tenant_id::text = current_setting('app.tenant_id', true)`)
- [x] Migration role separate from app role (`optiqor_migrator NOLOGIN BYPASSRLS` vs `optiqor_app NOLOGIN`)
- [x] **`migrations/0002_workload_observed_state.sql`** — additive columns on `workloads`: `container_image TEXT`, `current_cpu_request_millicores`, `current_memory_request_bytes`, `current_cpu_limit_millicores`, `current_memory_limit_bytes`, `replicas INT`, `has_hpa BOOL`, `last_observed_at TIMESTAMPTZ`. Partial index on `container_image` for cross-tenant pattern queries (used under `is_superuser_context()`); partial index on `(tenant_id, last_observed_at DESC)` for the recent-observed dashboard query. NULL-able by design (GitOps-parsed workloads have no observed state until the Phase-5 agent runs). Non-negative CHECK constraints on the numeric columns. _Shipped 2026-05-17._
- [x] **`migrations/0003_tenancy_primitives.sql`** — additive helpers, pure refactor of existing RLS policies (semantics unchanged): `uuid_generate_v7()` function (pure SQL, no extension dependency; existing v4 IDs stay) · `current_tenant_id()` helper reading `current_setting('app.tenant_id', true)` (**variable name preserved** to match `internal/platform/db` bind helper) · `is_superuser_context()` reader + `set_superuser_context(on, reason)` setter (the setter writes an `audit_log` row on every flip with reason mandatory) · `set_updated_at()` trigger function attached to `tenants`, `workspaces`, `recommendations`. All 10 baseline `tenant_isolation` policies rewritten to `USING (tenant_id = current_tenant_id() OR is_superuser_context())`. `audit_log.tenant_id` is now nullable to permit cross-tenant audit rows. _Shipped 2026-05-17._
- [x] **Design call: denormalised current-state on `workloads` vs. derive-from-`metric_samples`** — **DECIDED 2026-05-24: denormalise** (one writer = the agent reconciler; dashboard query speed wins; single source of truth is `(workload_id, last_observed_at)`). Decision captured as an inline comment on `0002_*` and locked by the columns shipped in that migration. Revisit only when the first dashboard latency budget bites.

### Observability (code + Helm complete; cluster install pending EKS)
- [x] Prometheus + Grafana + Loki + Tempo + OTel Collector Helm values in `deploy/helm/observability/`
- [x] SLO recording rules + alerts in `deploy/helm/observability/rules/optiqor-slo.yaml` (API uptime ≥ 99.5%, PR comment p95 < 45s, sandbox p95 < 3s, cost/PR < $0.40, Apply Fix success > 85%)
- [x] Sentry init shim in `internal/platform/telemetry` (no-op default; `NewSentryReporter` adapter ships in Phase 5)
- [ ] `helm install` of the observability stack on the live EKS cluster

### Cost visibility — tag from Day 1
- [x] Every env's Terraform `provider "aws"` block declares `default_tags` with `Project`, `Environment`, `Tenant`, `ManagedBy`
- [x] CI check (`scripts/check-terraform-tags.sh`) fails if any env file is missing the required tag keys; wired into `.github/workflows/ci.yml`
- [x] `terraform fmt -recursive -check` runs in CI
- [ ] Athena workgroup `optiqor-cost-attribution` + named queries for `cost_per_tenant`, `cost_per_workflow`, `cost_per_environment` (lands with first prod CUR ingest in Phase 6)

### Production-readiness baseline (Phase 1 — must land before Phase 2)

#### Multi-cluster + team hierarchy (architectural — retrofit later is expensive)
- [x] Schema: `tenants → workspaces → clusters → namespaces → workloads` with FKs and RLS policies (`migrations/0001_baseline.sql`)
- [x] **Stable workload identity** — `workload_hash` BYTEA column populated by agent as `sha256(cluster_id || namespace || kind || canonical(primary_selector_labels))`
- [x] **Cross-cluster workload class group** — `workload_class_group_id` for fleet-wide recommendations
- [x] Namespace-to-team mapping config per workspace (namespaces.team column + workspace-level rule config)
- [x] RLS policies extended with `workspace_id` available via `set_config('app.workspace_id', $1, true)` for finer enterprise scoping

#### Disaster Recovery foundations (code complete; apply pending live AWS)
- [x] RDS Postgres: `backup_retention_period = 35` enabled in `modules/rds-postgres` (PITR window)
- [x] S3 Cross-Region Replication encoded in prod env (`s3_receipts` and `s3_sandbox` declare `replication_destination_bucket_arn` to `us-east-2`)
- [x] KMS keys multi-region in prod (`module "kms" { multi_region = true }` for both data and Receipt signing)
- [ ] Apply against live AWS account; first restore drill scheduled for Phase 6 (per todo.md gap #8)

#### GDPR baseline (legal must-have for any EU customer Y1)
- [x] `internal/gdpr` — DSAR export + erase scaffolding with 30-day purge window + tombstones; retention windows for Prometheus snapshots (90d), LLM call logs (30d), Receipts (7y), audit log (7y)
- [x] DPA template + public subprocessor list (`docs/legal/SUBPROCESSORS.md` + `docs/legal/DPA-template.md`)
- [ ] DocuSign integration for DPA signing flow (Phase 5 alongside Stripe billing)

#### Operator coverage Layer 1 (folded forward from Phase 4)
- [x] `internal/operators/detector` — owner-reference walker classifies workloads as `direct` or `operator:<group>/<kind>`

#### Safety profiles (folded forward from Phase 4)
- [x] `internal/safety/environment` — env classifier with **fail-safe `EnvUnknown → prod`** and per-env `Profile` (aggressiveness, confidence floor, auto-merge eligibility, manual approval)

#### Onboarding state machine (folded forward from Phase 5)
- [x] `internal/onboarding` — 7-stage state machine with `Activated()`, `TimeToFirstReceipt()`, `HealthyTimeToFirstReceipt()`, `ProgressPercent()`; SLO constants (sandbox <3s, install→first comment <10min, install→first reco <30min, install→first Receipt <35d)

#### LLM defence (folded forward from Phase 4)
- [x] `internal/agent/llm/sanitizer` — Helm comment stripper + prompt-injection detector + `<USER_DATA>` wrapping for suspicious content

**Exit (code-level):** `make build` produces all 3 binaries · `go vet ./...` passes · `go test -race ./...` passes (~90 tests) · all RLS-scoped tables guarded server-side · webhook receiver verified · onboarding/safety/operator/sanitizer scaffolding live.
**Exit (infra):** `terraform apply` builds prod from scratch · tagged commit auto-deploys to staging via ArgoCD · `/healthz` returns 200 from prod · `gitleaks` clean. _Pending pre-seed AWS account binding._

---

## Phase 2 — Weeks 3–4: Public Sandbox

> **Backend scope only.** CLI-side parser, cost engine, shareable-URL hashing, and the `--share` upload client all ship in the [optiqor repo](https://github.com/optiqor/optiqor-cli); see [ROADMAP.md](ROADMAP.md) for the cross-repo view. The backend Phase 2 work is the sandbox **receiver** — the public HTTP endpoint that accepts uploaded analyses, deduplicates them by hash, and renders a stable share URL.
>
> **Status (2026-05-24):** Backend code surface closed. Migration 0004 + `internal/sandbox.PgStore` give us the production share storage path; `internal/platform/ratelimit` ships an in-memory limiter wired into `cmd/api` for `POST /v1/analyze` + `GET /r/{hash}`; `internal/sandbox/perf_test.go` enforces p95 < 3s on the 30-detector demo chart in every CI pass; ADR-0016 captures the frontend stack. The S3 store adapter is the only remaining `[ ]` strictly gated on the AWS account binding; the auth-gated dashboard shell is the open frontend item.

- [x] `internal/parser` — Helm values + templates parser. **Values normalisation is reused from `github.com/optiqor/optiqor-cli/pkg/parser`** (single source of truth — same `Workload` struct the CLI's detectors run against). _Shipped as a thin re-export shim ([`internal/parser/parser.go`](internal/parser/parser.go)); Kustomize / ArgoCD / Flux multi-source bundling lands with Phase 7's GitOps work._
- [x] `internal/sandbox/handlers` — public sandbox API: `POST /v1/analyze` and `GET /r/{hash}` ([`internal/sandbox/sandbox.go`](internal/sandbox/sandbox.go)). 1 MiB body cap, content-hash-addressed share URLs, mandatory ±40% accuracy disclosure on every response. _HTML rendering of `/r/{hash}` ships with `web/` below; JSON is live now._
- [x] `internal/cost` — sandbox-grade rule-based engine v0 ([`internal/cost/pricer.go`](internal/cost/pricer.go) + [`internal/cost/static_pricer.go`](internal/cost/static_pricer.go)). Calls `rules.All()` from the CLI's `pkg/rules` directly via `go.mod` (no fork); 7-region AWS pricing table; `Pricer` interface so the agent's `LivePricer` (Phase 5) swaps in without callers changing.
- [ ] Shareable report storage (S3 adapter) — bucket `optiqor-prod-sandbox` already provisioned in Phase 1 Terraform with KMS + 30-day lifecycle + CRR. Receiver writes to `internal/sandbox.InMemoryStore` in dev and to `internal/sandbox.PgStore` once the DB binds; the S3 adapter for >256 KiB payload promotion is a single-file change behind the existing `Store` interface and lands when the AWS account binds.
- [x] **`migrations/0004_shared_analyses.sql`** — Postgres lookup table for `optiqor.dev/r/<hash>`: `id` (DEFAULT `uuid_generate_v7()`), `hash` (unique URL slug), `payload_sha256`, `findings_json JSONB`, `source TEXT CHECK ('cli','sandbox')`, `view_count INT`, `created_at`, `expires_at`. Public-by-design (no RLS). In-row payload up to ~256 KiB enforced by `payload XOR payload_s3_key` CHECK; larger rows promote to S3 (Postgres row keeps lookup + expiry + view counting). 6 invariant tests in `migrations_test.go` pin the schema. Go binding shipped as `internal/sandbox/PgStore` against a narrow `PgExec` interface so pgx / database/sql / a test fake all satisfy it. _Shipped 2026-05-24._
- [x] **Rate limit middleware** — `internal/platform/ratelimit` with `Limiter` interface, `Memory` fixed-window limiter (60 req/min/IP for the sandbox, single-replica safe), `Middleware` HTTP wrapper (writes RFC 9110 `Retry-After`, fail-open on limiter error), wired into `cmd/api/routes.go` ahead of `POST /v1/analyze` and `GET /r/{hash}`. Redis adapter is the same `Limiter` shape and lands when ElastiCache binds in Phase 5. _Shipped 2026-05-24._
- [x] **p95 < 3s benchmark in CI** — `internal/sandbox/perf_test.go` runs the 30-detector demo chart in-process and asserts p95 stays under the 3s budget on every `go test -race ./...` run. Currently measured p50 ≈ 12ms / p95 ≈ 18ms on a single replica (170× headroom). `OPTIQOR_PERF=1` raises iteration count to 200 for local capacity work. _Shipped 2026-05-24._

### Web frontend — `web/` (proprietary, Next.js App Router)

> **Stack decision (de-facto sign-off — 2026-05-11):** Next.js 15 App Router + TypeScript strict + pnpm + Tailwind 4 + Geist Sans/Mono + TanStack Query + Zod (planned) + Auth.js (planned). Marketing, sandbox, and the (still-open) auth-gated dashboard live in `web/`. Public share pages (`/r/<hash>`) and Receipt verifier pages (`/v/<id>`) are **served directly by the Go API** through the Apache-2.0 `pkg/htmlrender` package from the CLI repo — they need no Next.js layer, must be raw-HTTP indexable for Slack/GitHub link previews, and share the exact rendering with `optiqor analyze --html`.
>
> **Why this split:** keeps proprietary code (dashboard, billing, auth) in `web/` while the report rendering stays Apache-2.0 in `optiqor-cli/pkg/htmlrender/` (single source of truth for "what an analysis looks like"). See [optiqor-cli/todo.md](https://github.com/optiqor/optiqor-cli/blob/main/todo.md#tier-1--launch-anchors-still-open) Tier 1 for the CLI-side commitments.
>
> **Status (2026-05-11):** scaffolding, brand system, marketing + sandbox + supporting routes (11 static pages), Go-served share/verifier pages, and `make dev` runner all shipped. Remaining `[ ]` items are the formal ADR write-up, the auth-gated dashboard shell, S3 / Redis adapters, and the perf benchmark — none block the customer-visible marketing + sandbox path that is live now.

- [x] **ADR-0016 — Frontend stack** ([`docs/adr/0016-frontend-stack.md`](docs/adr/0016-frontend-stack.md), 2026-05-24). Captures the Next 15 + Tailwind 4 + Auth.js + Go-served share-page split, the brand-token source of truth, the same-origin proxy, the Vercel → CloudFront migration path, and four rejected alternatives. _Originally tracked as "ADR-0001" but ADR-0001 was already allocated to `0001-three-tier-execution.md`; landed under the next free slot._
- [x] **`web/` scaffold** — Next.js 15 App Router + TypeScript strict + pnpm + Tailwind 4 + Geist (Sans + Mono). ESLint + Prettier on by default; production build produces 11 static pages with 0 errors. [next.config.ts](web/next.config.ts), [package.json](web/package.json).
- [x] **Brand system** — [`optiqor-cli/brand/tokens.json`](../optiqor-cli/brand/tokens.json) (Apache-2.0 single source of truth) imported by `web/src/lib/brand.ts`; CSS custom properties mirror the same palette in [`globals.css`](web/src/app/globals.css). Hero glyph + wordmark shipped inline as SVG so the brand renders without a binary asset hop. _Editorial × Engineering visual language: near-black ink scale, electric-cyan accent on data only, hairline borders, no gradients, no purple._
- [x] **Typed API client (`web/src/lib/api.ts`)** — hand-maintained TS shapes mirror every Go handler response. Default base URL is `""` so calls go same-origin through the Next.js rewrite. Migration to OpenAPI-generated types lands with the spec at `optiqor-cli/docs/api/openapi.yaml`.
- [x] **Sandbox page (`/sandbox`)** — paste-and-go: textarea → `POST /v1/analyze` → results panel with cost-first ordering, severity badges, share-URL row, `⌘+Enter` shortcut. [sandbox/page.tsx](web/src/app/sandbox/page.tsx) + [sandbox-client.tsx](web/src/app/sandbox/sandbox-client.tsx).
- [x] **Go-served `/r/<hash>` share page** — `internal/sandbox.Handler.Share` reads from the `Store` and renders via `pkg/htmlrender` by default; `Accept: application/json` or `?format=json` serves JSON. **`share_url` now derives from the request host** (or `OPTIQOR_PUBLIC_URL` override) so dev sees `http://localhost:3000/r/<hash>` automatically.
- [x] **Go-served `/v/<id>` Receipt verifier** — `internal/receipts.Handler.Verify` renders a self-contained HTML page with a live signature-status badge, the canonical payload, the base64url signature, and offline-verify instructions.
- [x] **Marketing routes** — `/`, `/pricing`, `/security`, `/how-it-works`, `/install`, `/docs`, `/about`, `/contact`, `/legal`. Hero terminal preview shows the cost-first CLI output verbatim; stats grid uses tabular-nums for a Bloomberg-terminal feel.
- [x] **Dev runner (`make dev`)** — `scripts/dev-app.sh` boots api (:8080) + web (:3000) under one Ctrl+C with prefix-tagged logs, `set -m` process-group teardown, and a pre-shutdown pid snapshot so `go run`'s re-parented child doesn't leak. `make bootstrap` covers docker + migrate + pnpm install for first-run.
- [x] **Same-origin proxy (`next.config.ts`)** — `/v1/*`, `/r/*`, `/v/*`, `/oauth/*`, `/webhooks/*`, `/healthz`, `/readyz` rewrites point at `OPTIQOR_API_UPSTREAM` (default `http://localhost:8080`). The browser never sees a cross-origin call, CORS never gates a sandbox request, and production matches dev under any reverse proxy that does the same.
- [x] **Worker registers all five workflows** — `cmd/worker` now binds `apply_fix`, `cost_spike`, `echo`, `receipt_issue`, `rollback_watchdog` with dev-grade dependencies (noop LLM, logging publisher / notifier / initiator, in-memory receipt store, ephemeral signer). Production replaces each binding behind the same interface. ([register.go](cmd/worker/register.go) + [bindings.go](cmd/worker/bindings.go))
- [x] **Auth.js + dashboard shell (`/app/*`)** — GitHub provider via next-auth v5 (GitLab is a one-config-block follow-up for Phase 8). `web/src/middleware.ts` gates every `/app/*` route and redirects unauth to `/signin?callbackUrl=<original>`. Dashboard shell at [`web/src/app/app/layout.tsx`](web/src/app/app/layout.tsx) with sidebar nav, sign-out via server action; first dashboard page at [`web/src/app/app/analyses/page.tsx`](web/src/app/app/analyses/page.tsx). Backend session JWT signer + `/v1/session/whoami` + `/v1/session/issue` ship in [`internal/auth`](internal/auth/) so the Phase-5 cutover from `X-Optiqor-Tenant` header to JWT extractor is a config flip, not a refactor. _Shipped 2026-05-24._
- [x] **Onboarding flow (`/install/*`)** — `/install` now layers a logged-in [`OnboardingWizard`](web/src/app/install/onboarding-wizard.tsx) client component over the existing CLI-quickstart panels. Wizard reads `/v1/onboarding/state` on mount, renders the 7-stage timeline with progress + SLO callouts, advances via `/v1/onboarding/transition`. Backend `/v1/onboarding/*` handlers in [`internal/onboarding/handler.go`](internal/onboarding/handler.go) wrap the existing state machine; `Service` lazily creates a `signed_up` record on first read so the wizard never sees a 404. In-memory store today; Postgres store swap follows the same `Store` interface shape as the sandbox. _Shipped 2026-05-24._
- [ ] **S3-backed `sandbox.Store`** — current `InMemoryStore` resets on restart; the production path goes through `PgStore` (shipped 2026-05-24) for the in-row payload and through this S3 adapter for promoted >256 KiB payloads. Phase-1 Terraform already provisions the `optiqor-prod-sandbox` bucket with KMS + 30-day lifecycle + CRR; the adapter is a single-file change once the AWS account binds.
- [x] **Rate limit middleware** — [`internal/platform/ratelimit`](internal/platform/ratelimit/) wired into `cmd/api/routes.go` ahead of `POST /v1/analyze` and `GET /r/{hash}`. Memory limiter (60 req/min/IP) for Phase 2; Redis-backed limiter swaps in at Phase 5 behind the same `Limiter` interface. Writes RFC 9110 `Retry-After`; fail-open on limiter error so a transient backend blip does not 503 the sandbox. _Shipped 2026-05-24._
- [x] **p95 < 3s benchmark in CI** — [`internal/sandbox/perf_test.go`](internal/sandbox/perf_test.go) runs in every `go test -race` pass. Measured p50/p95/p99 logged so a future regression shows up as a soft warning before it breaches the budget. Currently p95 ≈ 18ms (170× under the 3s budget). _Shipped 2026-05-24._
- [x] **OpenAPI spec (`optiqor-cli/docs/api/openapi.yaml`)** — full 17-path public surface with response schemas, three security schemes (tenantHeader / bearerAuth / sessionCookie), and per-route status-code matrices. [`scripts/check-openapi-parity.sh`](scripts/check-openapi-parity.sh) asserts every Go-registered route appears in the spec and vice versa; wired into `.github/workflows/ci.yml` as the `openapi-parity` job. The spec lives in the OSS CLI repo so the TS client + community tooling can generate against it without sharing proprietary code. _Shipped 2026-05-24._
- [ ] **Deployment** — Vercel preview deploys per PR (cheap, fast, free for Phase 2). Production initially Vercel; migration to self-hosted Next standalone behind CloudFront when SOC 2 binds (Phase 9).

**Sequencing recap (week 1-2 shipped, weeks 3-4 open):**

| Wk | Drop | Status |
| --- | --- | --- |
| 1 | `pkg/htmlrender` + `optiqor analyze --html` + Go `/r/<hash>` + `/v/<id>` | ✅ shipped |
| 2 | `web/` scaffold + `/sandbox` + brand system + dev-proxy | ✅ shipped |
| 3 | Marketing pages + dev runner `make dev` + worker workflow wiring | ✅ shipped (Astro docs site still open at [optiqor-cli/docs-site/](../optiqor-cli/docs-site/)) |
| 4 | Auth.js + dashboard shell + first dashboard page (Analyses list) | ✅ shipped 2026-05-24 |
| 4 | Migration 0004 + PgStore + ratelimit middleware + perf benchmark + ADR-0016 | ✅ shipped 2026-05-24 |
| 4 | Session JWT + onboarding HTTP API + onboarding wizard + OpenAPI spec + parity gate | ✅ shipped 2026-05-24 |

**CLI side already shipped (see [optiqor repo](https://github.com/optiqor/optiqor-cli)):**
- [x] Helm values parser, 30-detector engine, shareable-URL hashing, `--share` HTTPS upload client with graceful offline fallback

---

## Phase 3 — Weeks 5–6: Detectors + LLM Diff + CLI v0.1

> **Backend scope is the LLM-driven Apply Fix path.** The deterministic 30-detector library is the CLI's responsibility (per the OSS playbook hard rule: no LLM in the CLI); the backend imports/mirrors the same rule definitions so server-issued recommendations cite the exact same detector IDs.

- [x] **30 detectors mirrored server-side** — canonical implementations live in the CLI's public `pkg/rules` library and are imported directly via `go.mod` (`github.com/optiqor/optiqor-cli/pkg/rules` + `github.com/optiqor/optiqor-cli/pkg/parser`). No fork, no duplication — backend's `internal/cost` calls `rules.Run(workloads, rules.All())` against the same struct types the CLI emits. New detectors land in the CLI's `pkg/rules` first; a `go get -u github.com/optiqor/optiqor-cli` in the backend picks them up automatically. Golden parity tests in `tests/integration/cli_parity_test.go` assert the CLI binary and the backend produce the same `Finding` set for the canonical fixtures
  - Source: 15 cost + 15 security detectors, CIS Kubernetes Benchmark / NSA hardening guide aligned (see [ROADMAP.md](ROADMAP.md) Phase 3 detector tables)
- [x] `internal/confidence` — Low/Med/High banding ([`internal/confidence/classifier.go`](internal/confidence/classifier.go)). `Classifier` interface + `Heuristic` Phase-1 rule engine (sandbox vs agent mode, history-day threshold, signal corroboration, prior-dismissal de-escalation); the Phase-2 trained model swaps in behind the same interface without callers changing
- [x] `internal/agent/llm` — orchestrator ([`internal/agent/agent.go`](internal/agent/agent.go)) with `LLMClient` interface seam, sanitizer integration, system-prompt structure suitable for Anthropic prompt caching (stable cached prefix). _Real Anthropic SDK wiring is a one-file add behind the interface; deferred until the API key + caching dashboards land._
- [x] `internal/agent/budget` — $0.40/analysis cap enforced before every model call by `Composer.GenerateFix`; worst-case projector based on per-model rates so the gate never lets a too-expensive call hit the wire
- [x] LLM call accounting → `llm_calls` table — `BudgetRecorder` interface + `CallRecord` struct ([`internal/agent/agent.go`](internal/agent/agent.go)); table is already provisioned in `migrations/0001_baseline.sql`; the production recorder writes through the existing `internal/platform/db` Postgres pool
- [x] LLM-generated Apply Fix diff renderer ([`internal/prwriter/comment.go`](internal/prwriter/comment.go) + [`internal/worker/workflows/apply_fix.go`](internal/worker/workflows/apply_fix.go)); composer emits an `EXPLANATION:` + `DIFF:` protocol the prwriter parses; markdown render is deterministic + diff-stable (minute-truncated timestamps + stable finding sort)
- [ ] LLM canary: same prompt occasionally sent to Sonnet AND Haiku; outputs compared, divergence alerts (todo.md production-readiness gap #6 Layer 3) — requires two model adapters live in prod first

**CLI side already shipped (see [optiqor repo](https://github.com/optiqor/optiqor-cli)):**
- [x] 15 cost detectors + 15 security detectors firing on bundled demo (30/30)
- [x] Confidence band engine (`internal/rules.Confidence`)
- [x] CLI v0.1 build + `npm pack` proven (14 KB `@optiqor/cli` tarball); `release.yml` workflow drives publish on `v*` tag
- [x] Commands: `analyze`, `demo`, `diff`, `score`, `audit`, `compare` + `--version`/`--help`
- [x] ASCII + JSON output with mandatory ±40% accuracy disclosure
- [x] `--share` opt-in upload to `https://sandbox.optiqor.dev/api/v1/share` over HTTPS with 5 s timeout and graceful offline fallback (overridable via `OPTIQOR_SHARE_URL`)

### Operational scaffolding (folded forward — preempts Phase 5 onboarding friction)

These items move "what blocks the first customer install" out of Phase 5 and into Phase 3, where they can land in parallel with the LLM Apply Fix work. Each is small in isolation; together they remove the cliff between "code works" and "design partner installs."

- [x] **Helm chart scaffold for the agent** — `deploy/helm/optiqor-agent/` shipped 2026-05-24 (PR #16). `Chart.yaml` + `values.yaml` + 6 templates (Deployment, SA, ClusterRole+Binding, NetworkPolicy, Service, _helpers). Read-only RBAC per ADR-0008 (`get`/`list`/`watch` only on workloads + topology + HPA + PDB + VPA + Karpenter CRDs). NetworkPolicy: 0 inbound, egress to cluster-DNS + :443 + in-cluster Prometheus only. Footprint defaults 50m/64Mi requests, 200m/256Mi limits. Pod hardening: non-root (uid 65532), `readOnlyRootFilesystem`, `allowPrivilegeEscalation: false`, all caps dropped, `seccompProfile: RuntimeDefault`. mTLS secret reference is `optiqor-agent-mtls` (optional mount; Phase 5 fills cert material). `helm lint` clean. _kubeconform CI gate deferred to follow-up — needs the binary installed in the runner._
- [x] **Validation gate skeleton wired** — `internal/applyfix/gate/` shipped 2026-05-24 (PR #16). Four-stage chained `Pipeline.Run(ctx, tenancy.Context, Candidate) (Result, error)` (`template` / `conform` / `dryrun` / `post`); `Validator` + `Policy` interfaces. `NotImplementedValidator` stands in for every stage; `SkeletonPolicy` lets `Passed`+`NotImplemented` through, `StrictPolicy` requires `Passed`. Wired into `ApplyFix` workflow so dispatch literally cannot bypass the gate. Constructor panics on nil policy / zero validators (fail-closed boot). Flip `cmd/worker/register.go` to `StrictPolicy` at Phase-5 boot once real stages land.
- [ ] **`PHASE.md` per domain package** — every `internal/<pkg>/` and `cmd/<binary>/` gets a `PHASE.md` declaring: status (shipped Phase N / stub for Phase M / not yet started), what's blocking the next milestone, current test coverage. Auto-generated from `go test -cover` + a roadmap-extraction script, refreshed in CI. Closes the "90% aggregate coverage hides scaffold packages" smell from the 2026-05-18 architecture audit (3 hrs to author + script + first-pass population)
- [ ] **`make verify-docs-sync`** — extends `scripts/check-roadmap-sync.sh` (per ADR-0015) to also assert: (a) every strategy doc's most-recent amendment date in root `/docs/` matches the synced copy in `optiqor/docs/strategy/`; (b) every ADR's "Implementation status" footer references real files or marks "not yet shipped." Catches doc/code drift between sessions. Wired into CI `make lint` (2 hrs)
- [x] **Integration tests against stubbed abstractions** — `tests/integration/stubs_test.go` shipped 2026-05-24 (PR #16). Invokes every `internal/billing` source (`aws-cur`, `capacity`) and `internal/vcs.GitHub` stub (`PostComment`, `OpenPR`) through its interface, asserts `ErrNotImplemented` (never panic, never silent zero). Also pins the `billing.Registry.Register` → `Lookup` round-trip. Azure / Hetzner / GitLab / Bitbucket stubs slot in cleanly when their packages land (Phase 7-8).
- [x] **Doc-comment at the CLI/backend pricer divergence point** — `internal/cost/static_pricer.go` updated 2026-05-24 (PR #16) to pin the rationale: backend swaps to `LivePricer` at Phase 5, CLI keeps its own static table forever for OSS reproducibility; don't try to keep the two in sync.

---

## Phase 4 — Weeks 7–8: PR Writer + Apply Fix

> **Status (2026-05-11):** `internal/prwriter` markdown renderer + Apply Fix preview endpoint + `apply_fix` workflow are live ahead of schedule (folded forward from Phase 4 to give Phase 2's sandbox a real "what would the Apply Fix PR look like?" surface). The remaining `[ ]` items are real-PR-opening + the multi-stage gate that requires a live K8s cluster.

- [x] `internal/prwriter` — PR comment markdown renderer ([`internal/prwriter/comment.go`](internal/prwriter/comment.go)) + Apply Fix preview endpoint `POST /v1/apply-fixes` ([`internal/prwriter/handler.go`](internal/prwriter/handler.go)) + `apply_fix` workflow ([`internal/worker/workflows/apply_fix.go`](internal/worker/workflows/apply_fix.go)) wired into the in-memory dispatcher with a `PRPublisher` interface seam. Cost-first body layout matches the CLI brand voice; security findings render as a bonus subsection
- [x] **`migrations/0005_vcs_installations.sql`** — shipped 2026-05-24 (PR #17). Numbered 0005 (next free slot) since 0005-0008 placeholders for auth/metric_samples hadn't been used yet. Schema as described: `uuid_generate_v7()` id, `tenant_id` (RLS-scoped), `provider TEXT CHECK ('github','gitlab')`, `installation_id BIGINT`, `account_login`, `access_token_ciphertext BYTEA` (KMS-encrypted), `token_expires_at`, `installed_at`, `revoked_at`, `status` (active|suspended|revoked), `UNIQUE (provider, installation_id)`. Integration tests in `tests/integration/vcs_installations_test.go` (PR #18) pin cross-tenant RLS isolation, BYTEA ciphertext round-trip, and the UNIQUE constraint across tenants.
- [x] **Signed-token primitive** shipped 2026-05-24 (PR #21) — `internal/applyfix/token`: HMAC-SHA256 over `(tenant_id, apply_fix_id, aud, iat, exp)` with a shared secret; `NewIssuer/NewVerifier`, constant-time compare, expiry + audience binding. Lets every SaaS→agent endpoint authenticate without each one rolling its own. GitHub App installation private key in AWS Secrets Manager lands with the first live-AWS deployment.
- [x] **PR comment latency p95 < 30s instrumentation** shipped 2026-05-24 (PR #22). `internal/applyfix/latency` registers `optiqor_apply_fix_step_duration_seconds{step}` histograms with 100ms-60s buckets; the ApplyFix workflow records `parse`, `operator_gate`, `compose`, `gate`, `validator`, `render`, `publish`, `total` for every dispatch. SLO recording rules read `step=total` for the 30s budget; the per-step breakdown drives the Grafana panel that tells on-call which stage to look at when the budget breaches.
- [x] **Skeptic Mode toggle** shipped 2026-05-24 (PR #22). `ApplyFix.SkepticMode bool` flips two behaviours: gate stages must reach `StatusPassed` (NotImplemented is no longer let through), and any warn-level validator verdict becomes a hard rejection. Boot fails closed when `SkepticMode` is on but `Validator` is nil so a misconfigured deployment can't disable the stricter floor. Use for new tenants + new clusters until the analysis surface has earned trust.
- [x] `internal/operators/detector` — Layer 1 of operator-coverage engine; pure-Go owner-reference walker (Phase-1) + Apply-Fix dispatch gate shipped 2026-05-24 (PR #21). `ApplyFix.OwnerResolve` + `ApplyFixPayload.WorkloadOwners` plumb the walker into the workflow; operator-owned workloads short-circuit before `Composer.GenerateFix` even runs. Layers 2-4 lift coverage further at Phase-7/9.

### Tier-1 data sources (gate Apply Fix — must precede Phase 5 onboarding)
- [x] `internal/agent/k8s` shipped 2026-05-24 (PR #21). Single package carries the read-only reader interfaces + in-memory fakes for every Tier-1 source: `EventsReader`, `HPAReader`, `PolicyReader` (PDB + ResourceQuota + LimitRange in one snapshot), `VPAReader`, `KarpenterReader`. ADR-0008 compliance: every interface only exposes `get`/`list`/`watch` semantics. The agent-side client-go adapter lives behind a build tag and lands when `cmd/agent` boots against a live cluster (Phase 5).
- [x] `internal/cost/oomkilled` — Prometheus scrape interface + 7-day window shipped 2026-05-24 (PR #21). `Reader.Count`/`Recent` against a narrow `QueryClient` interface; `InMemoryClient` is the test double; PromQL targets `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}`. Wires into the validator pipeline's `oom-recent` check via `validator.ClusterSignals.OOMRecent`.

#### Agent-mode waste detectors (require cluster state — cannot live in CLI's `pkg/rules`)
- [x] `internal/methodology/detectors/orphaned_pvc` shipped 2026-05-24 (PR #21). `Detector.Analyze(samples []PVCRef)` returns one Finding per PVC with `Referenced=false && AgeDays >= MinAgeDays` (default 7). Real PVC + Pod volume scan lives in the agent's k8s reader.
- [x] `internal/methodology/detectors/stale_namespace` shipped 2026-05-24 (PR #21). `Detector.Analyze(snapshots []Activity)` flags namespaces with `WorkloadCount > 0 && CPU = 0 && Network = 0 && PodRestarts = 0` over the staleness window (default 30 days). Empty namespaces (`WorkloadCount = 0`) skip — they're not stale, just unused.
- [x] `pkg/rules/idle_workload` (CLI sandbox-mode approximation) — flags `replicas=0 && !HasHPA` as the static-analyzable subset of "idle workload." Shipped in optiqor-cli on 2026-05-18. Agent-mode version (Prometheus-grounded "no traffic, no CPU for 7d") lives in `internal/methodology/detectors/idle_workload_observed` below.
- [x] `internal/methodology/detectors/idle_workload_observed` shipped 2026-05-24 (PR #21). `Detector.Analyze(samples []Sample)` against the `IdleThreshold` (default MaxP95CPUMilli=10m, MaxNetworkB7d=1MiB). Replicas > 0 + no HPA + signals under threshold = finding. Reads `Sample` from the agent's k8s + Prometheus snapshot.

### Algorithmic improvement: Validation Before Recommendation (1 wk)
- [x] `internal/validator/` — shipped 2026-05-24 (PR #19). `Validator` interface with `Check(ctx, *tenancy.Context, Candidate) (*Verdict, error)`; `Pipeline.Run` returns the first hard rejection + every verdict for tuning. Reject-on-hard short-circuit.
- [x] Validators: `pdb`, `resourcequota`, `limitrange`, `hpabounds`, `dependency`, `oom-recent` (PR #19, shipped via `Default()`). HPABoundsCheck warns when in-bounds (the HPA dominates static replica edits) and hard-rejects out-of-bounds.
- [x] Wired into the ApplyFix workflow (PR #21). `ApplyFixPayload.ClusterSignals` + `.ProposedCPUMilli/MemoryB/Replicas` flow into `validator.Candidate`; `Pipeline.Run` runs after the gate, before Publish. Hard rejections short-circuit Publisher.Publish (fail-closed). Phase-5 wires real cluster signals via `internal/agent/k8s` readers; for now signals stay zero-valued on webhook paths and validators no-op as designed.
- [x] Rejected candidates logged with reason — shipped 2026-05-24 (PR #22). `validator.Pipeline.WithLogger(slog.Logger)` writes one INFO line per hard rejection with `tenant_id`, `validator`, `workload`, `detector_id`, `reason`. Detector-library tuning reads from the unified slog pipeline (Loki) — same JSON shape as every other domain log line.
- [x] Metric: `optiqor_validator_rejects_total{reason}` — shipped 2026-05-24 (PR #20). `validator.NewMetrics(registry)` pre-registers one counter per validator in `Default()` plus an `other` fallback; `Pipeline.WithMetrics(...)` makes the pipeline emit on every hard rejection. Prometheus alert (>2× WoW) is operational config; lands with the SLO dashboard wiring.

### Differentiator additions (folded into Phase 4)
- [x] `internal/prwriter/narrative` — shipped 2026-05-24 (PR #19). `Generator` interface + deterministic `TemplateGenerator` for dev/tests (no network egress). LLM-backed implementation (Haiku) lands when the real Anthropic SDK wires up.
- [x] `internal/cost/cis` — CIS Kubernetes Benchmark v1.9 control IDs per security detector (PR #17, 2026-05-24). `Controls(detectorID)` returns the slice; `Has(detectorID)` for the renderer's gate. Maps 9 security detectors.
- [x] `internal/prwriter/labels` — PR labels-as-policy parser (PR #17, 2026-05-24). Recognises `optiqor:skip`, `optiqor:budget=$X` (USD, optional `$` prefix), `optiqor:wait-for-prom=Nd|Nh|Nm`. Unknown `optiqor:*` labels surfaced so the comment renderer can warn on typos.
- [x] `internal/ingestion/coalesce` — shipped 2026-05-24 (PR #19). `Coalescer.Observe` against `(tenant, repo, chart-path)` key; first PR dispatches, second within TTL coalesces with the original PR URL, expired entries get replaced. Injected clock + 24h default TTL.

### Early kickoff — KMS Sign integration (folded forward from Phase 6)

The Receipt-signing path is the single most credibility-load-bearing feature in the product. Don't wait for Phase 6 to start integrating with AWS KMS — by then the schedule is too tight to handle KMS-specific gotchas (IAM scope, region selection, audit-log shape, error semantics). Two days of work now buys a Phase 6 that ships on time.

- [x] **AWS KMS `kms:Sign` swap in `internal/receipts/`** — `internal/receipts/kms` package shipped 2026-05-24 (PR #20). New `kms.Signer` implements the same `Sign(Receipt) (string, error)` shape; pre-hashes payload with SHA-256 and calls a narrow `KMSClient.Sign(ctx, keyID, digest)` interface. `kms.FakeKMS` is the deterministic test double; the aws-sdk-go-v2 wrapper lands behind a build tag when AWS binds. KeyID must encode the algorithm (`ecdsa-p256`) per ADR-0017 — `NewSigner` rejects mis-tagged keyIDs to fail-closed at boot.
- [x] **ADR-0017** — shipped 2026-05-24 (PR #20). Switches Receipt signing from Ed25519 to ECDSA P-256 because AWS KMS does not support Ed25519. Wire format unchanged; verifier dispatches on keyID prefix.
- [x] **Terraform module `infra/terraform/modules/kms-signer/`** shipped 2026-05-24 (PR #22). `aws_kms_key` with `customer_master_key_spec = "ECC_NIST_P256"`, `key_usage = "SIGN_VERIFY"`, `multi_region` flag, 30-day deletion window. Key alias encodes the algorithm tag (`-receipt-signing-ecdsa-p256`) so the Go signer's `NewSigner` keyID-check fails closed on a wrong-algorithm misconfiguration. Key policy scopes `kms:Sign` to a `signer_role_arns` allowlist and `kms:GetPublicKey`+`kms:Verify` to a `verifier_role_arns` allowlist (ADR-0011 §"Signer service isolation"). Replaces the looser receipt-signing key in `modules/kms` (kept for backwards compat; its description updated to redirect new deployments).

### Production-readiness — Apply Fix safety + LLM defense + environment classification (Phase 4)

#### Pre-merge validation gate (gates Apply Fix dispatch — must precede Phase 5)
- [x] `internal/applyfix/gate/render` — shipped 2026-05-24 (PR #17). Applies the LLM's unified diff to ChartYAML and YAML-parses the result; catches LLM corruption before the chart hits any real `helm template`. Real `helm template` lives in the agent's Phase-5 dryrun stage.
- [x] `internal/applyfix/gate/post` — shipped 2026-05-24 (PR #17). Deterministic post-validators: rejects diffs that drop a `labels:` key (breaks Service selectors / HPA targets) or shrink a CPU/memory request beyond the safety floor (default 50%, override via `MaxResourceReductionRatio`).
- [x] `internal/applyfix/gate/conform` — shipped 2026-05-24 (PR #19). Minimal apiVersion/kind shape check + RequiredKeys assertion on the post-diff chart values. Doesn't bundle a kubeconform binary at runtime — the agent's Phase-5 dryrun stage runs real kubeconform against the cluster's actual API version. CI does run `helm template | kubeconform -strict` against the rendered agent chart via `tests/integration/helm_chart_test.go` (PR #18).
- [x] `internal/applyfix/gate/dryrun` seam — shipped 2026-05-24 (PR #20). `DryrunValidator` + `AgentClient` interface (`DryRun(ctx, t, DryRunRequest) (DryRunResponse, error)`); `HTTPAgentClient` posts to the agent's `/v1/dry-run` with `Authorization: Bearer` + `X-Optiqor-Tenant`. Without an AgentClient configured it reports `NotImplemented` and `SkeletonPolicy` lets it through; flip to `StrictPolicy` + a real client once Phase-5 onboarding installs the agent. The signed-token + custom-admission-webhook test (Kyverno/Gatekeeper/OPA) lives on the agent side and lands with `cmd/agent`.

#### Prompt injection defense + LLM output validation
- [x] `internal/agent/llm/sanitizer` — folded forward into Phase 1 (see Phase-1 entry). Strips Helm template comments, detects prompt-injection patterns, wraps suspicious content in `<USER_DATA>` boundaries, enforces per-field length limits. Stays in this section as the Phase-4 entry it was originally scheduled under.
- [x] `internal/agent/llm/validator` — shipped 2026-05-24 (PR #17). Deterministic checks on the LLM's unified diff before it reaches the gate: rejects zero replicas, zero CPU/memory, missing hunk headers. Hard issues set `Result.Rejected`; warn-level issues feed confidence down-rank in the Composer. Schema-aware ±10× check lands when the post-stage exposes the pre-diff parsed values.
- [x] `internal/agent/llm/audit` — shipped 2026-05-24 (PR #17). `Auditor` interface + `InMemoryAuditor` + `NullAuditor`. `Record` carries `InputSHA256` + `OutputSHA256` (forensic trail, never the plaintext prompt/response) + model + cost + suspicious flag. Hash helper enforces SHA-256 hex at the call site so producers can't accidentally store plaintext.
- [x] `internal/agent/llm/canary` seam — shipped 2026-05-24 (PR #20). `Client` wraps two `LLMClient` adapters; Primary's response is what Composer sees, Secondary runs in a goroutine when `SampleDecider` approves and the resulting diff diff feeds a `Reporter`. `AlwaysSample` / `NeverSample` cover the canary-on / canary-off ends; production wires a 5% sampler. Real production reporter writes to Sentry; tests use `collectingReporter`. The end-to-end alerting plumbing (Sentry severity, oncall page) lands once Sentry binds (Phase-5 GAP item).

#### Environment classification (input to safety profiles)
- [x] `internal/safety/environment` — folded forward into Phase 1 (see Phase-1 entry). `Classify(...)` resolves environment from customRules → cluster label → ns label → cluster name → ns name; unknown maps to prod (fail-safe). `ProfileFor(env)` returns the `Profile` the cost engine consults.
- [x] **Per-environment aggressiveness in `internal/cost/strategy`** — shipped 2026-05-24 (PR #22). `strategy.For(env)` returns the `Sizing` bundle: `PercentileTarget` (P99 prod / P95 staging+dev), `MaxMemoryReductionPct` (10/25/0=unbounded), `MaxReplicaReductionPerPR` (0/2/100), `MinConfidence` (high/medium/low), `AutoMergeEligible`, `ManualApproval`. Helper methods `ConfidenceMeets`, `MemoryCutAllowed`, `ReplicaCutAllowed` return bool so the detector library + validator pipeline can branch without re-implementing the thresholds. Unknown env maps to the prod fail-safe profile.

---

## Phase 5 — Weeks 9–10: Design Partner #1 + Slack + Dashboards

- [ ] `cmd/agent` real watch loop: client-go informers + Prometheus scrape, mTLS to SaaS. **Critical path** — every "Optiqor reads X from the cluster" claim downstream depends on this binary going live. Engineer assigned by Phase 3 close, not Phase 5 start
- [ ] **Fill in the Phase-3 agent Helm chart** — replace `values.yaml` placeholders with real defaults; ship mTLS cert provisioning runbook; pre-flight checker invocation; add `helm install` / `helm upgrade` end-to-end test in `tests/e2e/agent/`. Chart-via-customer-GitOps update model per ADR-0008 — Optiqor publishes new chart versions, customer's ArgoCD / Flux reconciles on their schedule
- [ ] **Customer dashboard `/app/*` ship-list** (parallel-track Phase 5; frontend engineer starts at Phase 3 close, not Phase 5 start, because Auth.js wiring + tenant resolution + Analyses list page are 2 weeks of work that can't compress):
  - Auth.js + GitHub OAuth (extends Phase 2 shell with real session issuance via `/v1/session/whoami`)
  - Analyses list page (sortable / filterable React table; ~2 days)
  - Receipts browser with WebCrypto verifier (~3 days; depends on Phase 6 KMS Receipts but stub-renders against fixtures earlier)
  - Apply Fix history per workload (~2 days)
  - Cost spike timeline (~2 days)
  - Billing / usage panel (depends on Stripe; lands with `0008_stripe_mirror.sql`)
- [ ] Slack: digest workflow, `/optiqor status` slash command
- [ ] On-call docs + runbooks in `docs/runbooks/`

### Schema additions for auth + agent watch (Phase 5)
- [ ] **`migrations/0005_auth.sql`** — `users` (id DEFAULT `uuid_generate_v7()`, email CITEXT UNIQUE, name, auth_provider, auth_provider_id, created_at, last_login_at) — **global, no RLS** (a human can belong to many tenants; auth subsystem is sole reader/writer) · `memberships` (user_id, tenant_id, role CHECK ('owner','admin','member','viewer'), joined_at) — RLS-scoped · `api_tokens` (tenant_id, name, `token_hash BYTEA`, `scopes TEXT[]`, last_used_at, expires_at, revoked_at) — RLS-scoped. Token validation runs on the `optiqor_migrator BYPASSRLS` connection until tenant is resolved; then `set_config('app.tenant_id', ...)` flips to the regular pool. Gates the Phase-2 Auth.js shell going live with real sign-up
- [ ] **`migrations/0006_metric_samples.sql`** — TimescaleDB hypertable for raw agent metrics: `time TIMESTAMPTZ`, `tenant_id`, `cluster_id`, `workload_id`, `pod_id TEXT`, `cpu_usage_cores NUMERIC`, `memory_usage_bytes BIGINT`, `cpu_request_cores`, `memory_request_bytes`. `create_hypertable('metric_samples','time', chunk_time_interval => '1 day')` · compress `segmentby='workload_id', orderby='time DESC'`, `add_compression_policy(INTERVAL '7 days')` · `add_retention_policy(INTERVAL '35 days')` (30-day window + 5-day buffer) · continuous aggregate `metric_samples_hourly` materialising `avg / max / approx_percentile(0.95) / approx_percentile(0.99)` via TimescaleDB-toolkit `percentile_agg`; refresh `start_offset=35d / end_offset=1h / schedule=30min`. **Sizing engine reads the aggregate, not raw.** RLS via `tenant_id`. **No FKs** (hypertable convention); nightly cross-table sanity check verifies `metric_samples.workload_id ∈ workloads.id` and alerts oncall on drift
- [ ] **Design call revisit: `onboarding_progress` table vs `tenants.onboarding_state` JSONB** — current JSONB design (Phase 1) is fine while nudge cadence is hard-coded. Split into a dedicated table with one row per state transition when nudges become customer-tunable, per-stage SLA reporting is needed, or activation-funnel charting wants per-step time-in-state. Defer until Phase 5 nudges run against real tenants

### Tier-1 data sources (agent-resident — round out the data picture)
- [ ] `internal/agent/k8s/topology` — Service / Endpoints graph (workload→service→endpoint), exposed to backend for "no live traffic" detection (2 wk)
- [ ] `internal/agent/nodeprov/` — `NodeProvisioner` adapter (replaces single `internal/agent/karpenter`); detect at agent install via pre-flight, store class on `tenants.node_provisioner_class`:
  - `nodeprov/karpenter` — T1: NodePool + NodeClaim resource reader; high-confidence node math (1 wk)
  - `nodeprov/autoscaler` — T2: detect node-scaling shape and read accordingly (1 wk, expanded from the original spec to cover the three EKS shapes most customers actually run):
    - **(2a)** EKS Managed Node Groups via `eks:DescribeNodegroup` — AWS-managed ASG-with-CAS bundle. **This is the AWS default for new EKS clusters; most Year-1 customers will be on this.** Read instance type, capacity type (`on-demand`/`spot`), AMI version, taints, labels via the EKS API directly, not raw ASG.
    - **(2b)** Self-managed ASG + Cluster Autoscaler — detect `cluster-autoscaler` Deployment in `kube-system`; read ASG configs via `autoscaling:DescribeAutoScalingGroups` filtered by the `k8s.io/cluster-autoscaler/<cluster>` tag.
    - **(2c)** Standalone ASG (no CAS, target-tracking scaling policies) — detect ASGs tagged `kubernetes.io/cluster/<name>` without a CAS Deployment present. ASG-driven scaling is still scaling; classify as T2 (medium confidence), not T3.
  - `nodeprov/static` — T3: no autoscaler detected anywhere; render manual-step recommendations; cap confidence at Medium (3 days)
  - `nodeprov/managed/aks` — Phase 7 (AKS node pools)
  - `nodeprov/managed/hetzner` — Phase 8 (Hetzner Cloud node pools)
  - `nodeprov/managed/gke` — **Year 2** (GKE Node Auto-Provisioning + Standard node pools). Schema enum `managed-gke` already exists in `migrations/0001_baseline.sql:93` as a placeholder; until the adapter ships, the pre-flight checker (below) **fails closed** rather than routing GKE customers to `static`. ~2 wk based on the AKS adapter's expected scope; needs separate auth path (GCP service account + workload identity), separate API client (`google.golang.org/api/container/v1`), separate billing connector. Lands when GCP customers become a deliberate go-to-market — not before
- [ ] `internal/onboarding/preflight` — **fail-closed routing for unsupported provisioners** (2 days). When pre-flight detects GKE NAP or any other provisioner without a Year-1 adapter (e.g. OpenShift Machine API, DigitalOcean K8s), the installer rejects the agent install with an explicit error: *"Optiqor doesn't support <provisioner> yet — track at github.com/optiqor/optiqor/issues/<N>. Year-1 supported: Karpenter, EKS MNG, EKS+CAS+ASG, standalone ASG, on-prem/static."* No silent fallback to `static` (that mis-classifies the customer's bill basis and corrupts Receipt accuracy). The `managed-gke` schema enum value stays as a placeholder; the route is the gate.
- [ ] `internal/cost/strategy` — node-provisioner-class is an input; sizing strategies vary by tier (Karpenter can recommend rapid scale; static node groups cannot)

### Decision/orchestration layer — gaps surfaced by the ADR audit (Phase 5)
- [ ] `internal/methodology/conflict/` — VPA + HPA conflict resolver (per ADR-0012). Reads VPA's mode (Off / Initial / Auto) from `internal/agent/k8s/vpa` and the HPA spec from `internal/agent/k8s/hpa` for the same workload. Three outcomes: (a) no VPA or VPA Off → Optiqor produces canonical recommendation; (b) VPA Off but configured → Optiqor reads its recommendations as one signal, may override with HPA-aware sizing; (c) VPA Auto → Optiqor disables itself for that workload and surfaces a warning in the dashboard. Pure functions; tests pin every combination (1 wk)
- [ ] `internal/methodology/hparec/` — HPA target-utilization recommender. When a workload has HPA configured with `targetCPUUtilizationPercentage` and statistical sizing detects memory pressure (not CPU pressure), recommend switching to a custom metric or adjusting the target. PR shape differs from workload PR (modifies HPA spec, not Deployment) — uses the Karpenter-NodePool PR routing planned for Phase 7 (1 wk)
- [ ] **Spec: trust-spectrum × env-aware composition** (`docs/specs/trust-modes-composition.md`). ADR-0009 introduces Suggest / Propose / Auto-merge as the per-tenant trust spectrum; `internal/safety/environment/` defines env-aware aggressiveness (prod conservative, staging moderate, dev aggressive). These compose orthogonally — and how is currently undocumented. Spec must answer: does "Auto-merge mode" mean Auto for prod, or only for staging/dev? Does "Suggest mode" override env aggressiveness, or layer on top? Resolution lands in this spec before Phase 5 closes — without it, the first Auto-merge customer hits an undefined edge case (3 days)

### Differentiator additions (folded into Phase 5)
- [ ] `internal/notify/slack/diff` — render Apply Fix diff inline in Slack thread for mobile-first review (3 days)
- [ ] `internal/integrations/argocd-notifications` — accept ArgoCD Notifications webhook back into our pipeline; close the Apply Fix → merge → sync → measure → Receipt loop (3 days)

### Operational backbone — onboarding + optiqor-on-optiqor (Phase 5)
- [ ] `internal/onboarding/` — state machine (`signed_up → vcs_connected → repo_selected → first_pr_analyzed → agent_installed → first_apply_fix → first_receipt_issued`); each transition timestamped in `tenants.onboarding_state` JSONB column
- [ ] `internal/onboarding/preflight` — pre-flight checker reads cluster K8s version, Prometheus presence, RBAC, Karpenter, PDB/RQ counts; renders preview page before `helm install`
- [ ] `internal/onboarding/nudges` — Temporal cron workflows: 24h no-VCS email · 72h no-agent in-app prompt · 7-day no-Apply-Fix CSM/Slack alert
- [ ] `internal/onboarding/demo` — synthetic-but-clearly-labeled demo data path for clusters with <30 days of Prometheus history
- [ ] `cmd/api` route `/onboarding/health` — per-tenant funnel position + blockers; shareable with the customer
- [ ] **Optiqor-on-Optiqor install** against our own EKS — production GitHub App, in-cluster agent on `prod` cluster, every PR to `backend/` gets a Optiqor comment (zero engineering effort beyond using the product)
- [ ] `internal/metrics/activation` — Activation Rate (≥60% target) + Time to First Receipt (≤35 days p50) computed daily

### Hard SLOs to enforce in Phase 5
- [ ] Sandbox p95 < 3s (already in Phase 2, re-verify)
- [ ] App install → first PR/MR comment < 10 min p95
- [ ] Agent install → first recommendation < 30 min p95
- [ ] Agent install → first Cloud Receipt < 35 days p50

### Production-readiness — recommendation lifecycle + blast-radius (Phase 5)

#### Recommendation lifecycle (drives churn if missing)
- [ ] `internal/recommendations/lifecycle` — five-state machine: `active` / `snoozed` / `dismissed` / `ignored-workload` / `ignored-class`
- [ ] PR-label parser handles `optiqor:snooze=14d` (label-driven snooze)
- [ ] "Dismiss" button on PR comment writes to lifecycle store; reason captured for detector tuning
- [ ] Dashboard toggles for `ignored-workload` and `ignored-class`
- [ ] **Drift detection** — agent compares cluster state vs last-known-recommendation; if customer applied manually, mark `applied-externally` and route through measured-delta → Receipt path
- [ ] **Recommendation expiration** — Temporal cron auto-expires recommendations >60 days old with "data is stale, re-running"
- [ ] **Re-emergence rules** — `dismissed` re-fires only on evidence change (new OOM, P95 jumped 20%, etc.); reason noted in re-emerged comment
- [ ] `recommendations.dismissals` table — every dismissal logged with reason; powers detector-tuning dashboard ("`cpu_overprovision` dismissed 80% of the time on `web-steady` — fix the heuristic")

#### Blast-radius scoring (depends on Service/Endpoints topology graph)
- [ ] `internal/safety/blastradius` — score 1–5 per recommendation using topology graph from `internal/agent/k8s/topology`
- [ ] Score gating: 1 (auto-merge in dev), 2 (standard PR), 3 (manual approval), 4 (manual + senior reviewer), 5 (Apply Fix disabled, recommendation only)
- [ ] Customer-configurable per-namespace overrides (`payments-prod` → always blast-radius 5)
- [ ] Blast-radius surfaced prominently in PR comment header so reviewers see scope before reading details

---

## Phase 6 — Weeks 11–12: Receipts + Cost Spike + Partners #2–3

> **Status (2026-05-11):** Deterministic core landed early — Receipt signing/verification, the rollback watchdog state machine, the cost-spike webhook, and the CUR-row parser are all live and tested. The remaining `[ ]` items wire the Athena query path (needs the AWS account), KMS-backed signing (currently uses process-local `ed25519` keys), and the transparency log.

- [x] AWS CUR row parser ([`internal/ingestion/ingestion.go`](internal/ingestion/ingestion.go) — `ParseCURRows`) + `POST /v1/ingest` receiver that routes parsed rows through a sink interface. _Athena query orchestration → daily-partition workflow is the remaining `[ ]` and requires the AWS account_
- [x] `internal/receipts` — Ed25519 signing + canonical-JSON encoder + tampered-payload-/sig-/key tests + public verification endpoint `GET /v1/receipts/{id}` ([`internal/receipts/receipts.go`](internal/receipts/receipts.go) + [`internal/receipts/handler.go`](internal/receipts/handler.go)). Production swap to KMS-asymmetric signing replaces only the `Issuer` struct's key source
- [x] Cost Spike detector workflow ([`internal/worker/workflows/cost_spike.go`](internal/worker/workflows/cost_spike.go)) + `POST /v1/cost-spikes` webhook ([`internal/billing/spike_handler.go`](internal/billing/spike_handler.go)); threshold filter + `SpikeNotifier` seam for the eventual Slack adapter
- [x] `internal/rollback` — 7-day post-merge metric watchdog ([`internal/rollback/watchdog.go`](internal/rollback/watchdog.go)) + `rollback_watchdog` workflow ([`internal/worker/workflows/rollback_watchdog.go`](internal/worker/workflows/rollback_watchdog.go)); decision logic is pure functions over a snapshot stream so the Phase-7 Prometheus poller can replay history deterministically. Auto-rollback PR opener seam (`RollbackInitiator`) ready for the GitHub adapter

### Engineering hygiene — cost-engine reference reading (private, internal-only)

OpenCost (CNCF Incubating, Apache 2.0) is a useful **private reference** when building `internal/cost/attribution` and the CUR Athena query path. Their codebase has already survived the edge cases we're about to discover (Spot-interruption windowing, idle-capacity partitioning, SP/RI amortization, NAT/ALB line-item shapes). Engineers building the cost engine may read OpenCost source to understand the *concepts*, then write Optiqor's own implementation from a clean slate. This stays internal — it's an accelerant, not a positioning story.

- [ ] **Read, don't copy.** Take the algorithmic concepts. Not the code, not the struct names, not the function signatures, not the control flow. Same way you'd read Postgres source to understand MVCC before building a database — concepts are free, structure is a liability.
- [ ] **Translate before writing.** Budget one engineer-day between "read OpenCost" and "open editor for `internal/cost/attribution`". Sketch the algorithm in Optiqor idioms (`*tenancy.Context` first arg, `Pricer` interface seam, our struct names) on paper or in a private design doc. Implement from the sketch with the OpenCost tab closed.
- [ ] **Reviewer's job: flag structural similarity.** During Phase 6 cost-engine PRs, reviewers scan for uncannily similar identifiers and control flow vs. upstream. Apache 2.0 is permissive but structural copying is an optics liability we don't need. If a PR feels too close, the fix is rename + restructure, not commentary.
- [ ] **Never surfaces in customer-facing artifacts.** Receipts, methodology page (`optiqor.dev/methodology/hybrid-v1`), marketing copy, pitch docs, dashboard tiles, PR comments — all Optiqor branding end to end. The reference reading lives in engineers' heads and possibly a private design doc under `docs/internal/`; nothing about OpenCost lineage ships in any artifact a customer, prospect, or auditor sees. See [docs/strategy/technical_implementation.md §7](docs/strategy/technical_implementation.md) for the customer-facing methodology framing — note the absence of any OpenCost mention.
- [ ] **No runtime dependency.** `go.mod` must not reference `github.com/opencost/opencost` (or any of its subpackages) at any point. Add `opencost` to the prohibited-imports linter list in the same PR that lands the first `internal/cost/attribution` file; CI grep guard catches accidental re-introduction.

### Recommendation engine — math packages (Phase 6, per ADR-0006)

These ship under `internal/methodology/` (not `pkg/`, per ADR-0006). Pure functions, no I/O, no clock reads — `Clock` interface injected. CI lint guard prevents `internal/methodology/` from importing other `internal/` packages, landing in the same PR as the first methodology file.

- [ ] `internal/methodology/sizing/` — statistical sizing engine (3 wk core, 1 wk integration). For each (workload × time-series), produce a recommended request/limit pair. Algorithm:
  - **CPU recommendation:** pull 30d hourly P95 from `metric_samples_hourly` continuous aggregate (per `migrations/0006_metric_samples.sql`); branch by class from `internal/methodology/classify/` (steady → P95 + 30% margin; daily-cyclical → max(seasonal-bucket-P95) + 25%; weekly-cyclical → same with 168h bucket; bursty → P99 + 50%); lower-bound at `current × 0.1` (never recommend >10× reduction in one PR); upper-bound at `current × 5` (never recommend >5× increase without human review).
  - **Memory recommendation:** same shape but P99 not P95 (memory is unforgiving); apply **lognormal correction** — fit a lognormal distribution to the observed series and take the 99th percentile of the fit, not the raw P99 (handles fat tails properly).
  - **OOMKilled awareness:** consult `internal/cost/oomkilled` 7-day history; if non-zero events, multiply safety margin by 1.5 and refuse to lower the request below the highest observed pre-kill memory value.
- [ ] `internal/methodology/sizing/lognormal.go` — lognormal-fit helper (gonum/stat); shipped as part of the sizing package but in its own file because the math is the part most worth testing in isolation. Tests: synthetic series with known lambda, observed series from production fixtures, edge cases (zero variance, single sample, large outliers).
- [ ] `internal/methodology/conflict/` — VPA + HPA reconciliation (cross-references Phase 5 entry above; lands here if the Phase 5 timeline slips, but the design lives in Phase 5).
- [ ] **Confidence band v2** — replace heuristic Low/Med/High in `internal/confidence/classifier.go` with statistical confidence derived from sample count + signal stability + seasonality match + OOMKilled history. Same `Classifier` interface; v2 is a drop-in. v1 remains for sandbox mode (no observed data). Phase 5 sandbox stub ships as v1; Phase 6 statistical replaces it for cluster-installed customers.

#### Phase 6 — scoping reality

The recommendation engine above is **4-6 engineer-months of work** when written conservatively (good test coverage, OOMKilled handling, lognormal memory math, classifier integration, dashboards). Phase 6's 2-week box currently treats it as a single line item, which is wrong.

Three options, pick before Phase 6 starts:

1. **Cut scope — ship `hybrid_v0` in 2 weeks:** P95 + 30% safety margin for CPU, P99 + 50% for memory, no lognormal correction, no class-aware branching, no OOMKilled multiplier. Sandbox-grade math that is honest about its limits in the Receipt's methodology field. Hybrid_v1 (full math) ships in Year 1 H2 (Phases 7-8). **This is the recommended path** — it gets Receipts shipping at the Phase 6 exit and earns trust on the limits.
2. **Extend Phase 6** — relabel as "Receipts foundation" (Weeks 11-22, ~3 months). Push Phase 7 onward back accordingly. Honest but expensive against the Year-1 GTM calendar.
3. **Compress hybrid_v1** — same scope, half the time, ship at lower quality. **Don't do this** — the recommendation engine is what gets signed in Receipts; quality directly maps to customer trust.

Decision goes in a follow-up ADR-0016 ("Recommendation engine scope-and-sequencing for Year 1"). Until that ADR lands, the Phase 6 plan above is **provisional**.

### Differentiator additions (folded into Phase 6)
- [ ] Free-tier monthly verified Receipt enabled in `internal/receipts` (no code change beyond plan-gating; ~1 wk to wire billing+plan logic + viral-share marketing copy)
- [ ] `internal/cost/shadow` — shadow-recommendation infra: detectors fire and outcomes are logged without posting to PRs; powers Year-2 numerical Confidence Scores (1 wk)

### Operational backbone — Receipt verification + webhooks + cost dashboard (Phase 6)

#### Receipt Verification Flow (must ship with the first Receipt)
- [ ] **AWS KMS asymmetric `SIGN_VERIFY` key** for Ed25519 signing — private key never exists outside the HSM
- [ ] `internal/receipts/signing` — sign every Receipt inside KMS; never load private bytes into app memory
- [ ] `internal/receipts/tlog` — Sigstore Rekor-style transparency log; every Receipt appended to a Merkle log; cannot be retrofitted
- [ ] `cmd/api` routes `/verify/<id>` (HTML page) + `/api/v1/receipts/<id>/verify` (JSON) — anonymous, public, no auth
- [ ] Browser **WebCrypto** verification — verify Receipt locally without server roundtrip
- [ ] **`@optiqor/verify` CLI** (npm, Apache 2.0, lives in `cli/` repo) — fetches public key via DID/HKP, validates Ed25519 offline, exit code 0/1 for CI gating
- [ ] Yearly key rotation procedure documented; old keys remain valid forever via tlog
- [ ] Documented + pen-tested compromise procedure (revoke in tlog → rolling-shadow re-sign → 24h customer alert)
- [ ] Stable Receipt YAML schema versioned at `methodology.optiqor.dev/<methodology>/<version>`

#### Webhooks (cheap and high-value)
- [ ] `internal/api/webhooks` — outbound event dispatcher; events: `receipt.issued`, `apply_fix.merged`, `cost_spike.detected`, `rollback.opened`, `validator.rejected`, `health_score.changed`
- [ ] HMAC-SHA256 signing with per-tenant secret rotation
- [ ] Exponential backoff retry; 30-day replay buffer in S3
- [ ] Per-tenant webhook health dashboard (delivery rate, average latency, last-failure reason)

#### Internal cost dashboard + Cost Council
- [ ] Daily Athena queries → Grafana panels: `cost_per_tenant`, `cost_per_workflow`, `cost_per_apply_fix`, `cost_per_llm_call{model=haiku|sonnet|opus}`, `cost_per_receipt`
- [ ] Alert when per-PR cost > $0.40 (already a SLO)
- [ ] **Monthly Cost Council** — engineering + finance review, top-3 line items to cut, sign off on next month's envelope (calendar invite, not engineering work)
- [ ] LLM cost guardrails: per-tenant budget enforcement in `internal/agent/budget`; weekly review of top-10 most expensive prompts
- [ ] Idle-resource auto-shutdown: staging scales to zero overnight + weekends (Karpenter scheduled disruption)

### Production-readiness — Stripe billing + DR drills (Phase 6)

#### Stripe billing (revenue blocker — must ship before Phase 6 paid customers)
- [ ] `internal/billing/stripe` — subscription lifecycle, customer portal integration, invoice generation
- [ ] **`migrations/0007_billing_line_items.sql`** — TimescaleDB hypertable for parsed bill rows (lands alongside or just before `0008_stripe_mirror`, when CUR ingestion graduates from `internal/ingestion`'s in-memory parser to Postgres). Schema: `time TIMESTAMPTZ`, `tenant_id`, `cluster_id`, `resource_id TEXT`, `instance_type`, `usage_type`, `cost_usd NUMERIC`. **Two enum columns mandatory from row one — impossible to retrofit cleanly:** `source TEXT CHECK ('aws_cur','azure_cost_mgmt','hetzner_invoice','capacity_deferred')` (which bill; drives 3-tier Receipt routing per [docs/idea.md amendments](../docs/idea.md)) and `pricing_mode TEXT CHECK ('spot','on_demand','savings_plan','reserved','other')` (rate within the bill). `create_hypertable('billing_line_items','time', chunk_time_interval => '1 day')` · compress `segmentby='cluster_id', orderby='time DESC'`, `add_compression_policy(INTERVAL '7 days')` · `add_retention_policy(INTERVAL '365 days')` (financial data; Receipts cite it). RLS via `tenant_id`. No FKs (hypertable convention)
- [ ] **`migrations/0008_stripe_mirror.sql`** — local mirror of Stripe state so billing UI never hot-paths Stripe. **Additive col on `tenants`:** `stripe_customer_id TEXT UNIQUE` (one human customer = one Stripe customer; subscriptions can change over time). Plus: `subscriptions` (tenant_id, stripe_subscription_id UNIQUE, plan, status CHECK ('trialing','active','past_due','canceled'), trial_ends_at, current_period_end, seats_or_clusters_limit), `usage_records` (tenant_id, metric CHECK ('clusters','workloads','receipts'), quantity, period_start, period_end, reported_to_stripe_at), `invoices` (tenant_id, stripe_invoice_id UNIQUE, amount_usd_cents, status, period_start, period_end, pdf_url). Reconciliation via Stripe webhook → `internal/api/webhooks`
- [ ] `internal/billing/meter` — usage metering reports cluster-count, workload-count, Receipt-count to Stripe Billing on the right axis per plan
- [ ] `internal/platform/plans` — plan-limits enforcement (Free: 2 clusters / 1 Receipt-per-month · Team $500/mo: 5 clusters / unlimited · Enterprise: custom)
- [ ] **14-day trial flow** for Team tier with Temporal-driven nudges at T-3 / T-1 / expiry; auto-downgrade to Free at expiry
- [ ] Annual billing with 15% discount; auto-invoicing for Enterprise contracts
- [ ] Stripe Tax integration for EU/UK/AU VAT (required for Hetzner customers)
- [ ] Plan-change webhooks (`subscription.upgraded`, `subscription.downgraded`, `trial.ended`) emitted via existing `internal/api/webhooks`

#### Disaster Recovery drills (SOC2 evidence)
- [ ] `cmd/worker` Temporal cron: monthly automated restore drill — restore last night's snapshot to scratch RDS, run schema integrity check, report time-to-restore; oncall paged on failure
- [ ] **Backup integrity** — every snapshot's hash signed with the same KMS key family as Receipts; integrity verifiable independently of AWS
- [ ] `docs/runbooks/disaster-recovery.md` — step-by-step recovery procedure; first fire drill executed and timed
- [ ] Quarterly fire drill schedule on team calendar; rotation of drill leader so anyone can lead

---

## Phase 7 — Months 4–6: Flux CD + Azure AKS + Cloud Receipt v2 + Workload Classification

- [ ] `internal/parser/gitops/flux` — read Flux `Kustomization` + `HelmRelease` resources
- [ ] `internal/billing/azure` — Azure Cost Management API connector (managed-identity auth)
- [ ] AKS-specific cost detectors (B-series burstable misuse, Azure Disk overprovisioning, Spot VM eligibility)
- [ ] `internal/receipts` extended for AKS — Ed25519-signed receipt against Azure invoice
- [ ] Cost Spike + Auto-Rollback parity for AKS clusters
- [ ] First AKS design partner onboarded (Apply Fix merged + Receipt issued)

### Algorithmic improvement: Workload Classification (3 wk)
- [ ] `internal/workload/classifier/` — workload partitioner using K8s labels, image pattern matching, and Prometheus metric-signature heuristics
- [ ] Classes: `web-steady`, `worker-bursty`, `batch`, `stateful-db`, `ml-inference`, `unknown`
- [ ] Per-class strategies in `internal/cost/strategy/` — each detector picks a strategy based on classified type:
  - `web-steady` → P95 right-sizing, aggressive
  - `worker-bursty` → P99.9 peak-coverage, conservative
  - `batch` → request-only, no limit suggestion
  - `stateful-db` → Apply Fix disabled for memory cuts; CPU only
  - `ml-inference` → GPU-aware sub-detector library
  - `unknown` → fall back to current Year-1 logic, flag for human review
- [ ] Classification result attached to every recommendation and stored in `recommendations.workload_class` for retrospective accuracy tracking
- [ ] Migration: backfill existing tenants' workloads in a single Temporal workflow
- [ ] Metric: `optiqor_recommendation_accuracy_by_class` — weekly accuracy lift dashboard

### Coexistence integration + math depth (Phase 7, per ADR-0012 + ADR-0009)

- [ ] **`internal/methodology/rollback/` — MOAT-CRITICAL.** Auto-rollback statistical math package (per `docs/strategy/technical_implementation.md §8.7` and `docs/strategy/business_strategy.md §8.4 moat #2`). This is the single component the rest of the K8s ecosystem **structurally cannot** ship — VPA / HPA / Karpenter react to symptoms, not causes, and have no pre/post change-attribution baseline. Customer keeps their existing autoscalers; we add the one thing those tools can't give them. **Do not deprioritize, do not compress at lower quality, do not ship without the full math pipeline.** If Phase 7 budget pressure forces a cut, cut something else and protect this. Replaces the Phase-5 `SimpleStats` bound-vs-snapshot comparison with the full statistical pipeline (4 wk):
  - `boxcox.go` — Box-Cox transform with lambda estimation. Pure functions, golden tests on synthetic series + production fixtures
  - `stl.go` — Seasonal-Trend-Loess decomposition at 24h and 168h periods. The stronger seasonality wins per signal
  - `changepoint.go` — PELT (Pruned Exact Linear Time) change-point detection. Used for locality check ("did the underlying distribution shift within 2h of deployment?")
  - `score.go` — combines transform + decomposition + change-point into a single `DeviationScore`. Watchdog state machine consumes this via the `Stats` interface
  - `stats.go` — `Stats` interface and `DeviationScore` struct so Phase-5 `SimpleStats` and Phase-7 `FullStats` are drop-in swappable behind the watchdog
- [ ] `internal/methodology/sequencing/` — fleet-level recommendation sequencer (2 wk). When multiple recommendations across a customer's workloads land in the same window, pick a safe order: (a) batch by repository so fewer PRs land per repo per day; (b) order by blast-radius score ascending so the cheapest-to-rollback ships first; (c) cool-off period per repository (max 3 Optiqor merges per repo per day, customer-configurable per ADR-0009 open question). Cross-workload coordinator workflow in `internal/worker/workflows/sequencer.go`
- [ ] `internal/prwriter/karpenter` — Karpenter NodePool PR routing. When a recommendation targets `NodePool` CRDs (different repository, different shape, different review needs than workload YAML), open a separate PR with the CRD-targeted template (1 wk). Triggered by recommendations from the Phase-5 `internal/agent/k8s/karpenter` reader; uses the deterministic-PR-shape templates from `tech_impl §5.2` with a NodePool variant

### Differentiator additions (folded into Phase 7)
- [ ] `internal/parser/helmfile` — Helmfile reader (declarative state of multiple Helm releases); opens self-managed platform-team segment (2 wk)

### Operator-Managed Workload Coverage — Layers 2 & 3 (Phase 7)
- [ ] `internal/operators/schema` — Layer 2 generic CRD-aware advice: read CRD OpenAPI schemas via `apiextensions.k8s.io`; heuristic field-path resolver for `spec.resources`, `spec.replicas`, `spec.template.spec.containers[*].resources`, `spec.podSpec.resources` (1 wk)
- [ ] `internal/operators/advice` — render copy-pasteable YAML patch + explanation for any operator we don't have a preset for; customer applies manually, agent measures outcomes, Receipt issues normally
- [ ] `internal/operators/presets` — Layer 3 hand-curated presets for top-5 Y1 operators (1 wk):
  - `presets/prometheus-operator` — `Prometheus`, `Alertmanager`, `ServiceMonitor`; never recommend memory cuts during WAL compaction
  - `presets/kube-prometheus-stack` — Helm-installed; `values.yaml` knob targets
  - `presets/cert-manager` — `Certificate`, `Issuer`; CPU sizing only, memory is heap-bound
  - `presets/strimzi` — `Kafka`, `KafkaTopic`, `KafkaUser`; per-broker resource math; never auto-fix StatefulSets
  - `presets/istio` — `IstioOperator`, `Gateway`, `VirtualService`; sidecar resource recs bounded by mesh-wide policy
- [ ] Coverage SLO instrumented: `optiqor_workload_coverage_ratio` per tenant; alert if a tenant's ratio < 90% (signals a missing preset for an operator they're using heavily)

## Phase 8 — Months 6–9: GitLab + Hetzner Cloud K8s

- [ ] `internal/vcs/gitlab` — OAuth, webhook receiver, MR comment renderer, signed-token Apply Fix → MR opener
- [ ] GitLab CI/CD integration parity with GitHub Actions Marketplace listing
- [ ] `internal/billing/hetzner` — Hetzner Cloud invoice connector (flat-rate per-server math)
- [ ] Hetzner-specific detectors: dedicated-vCPU (CCX) vs shared-vCPU (CX) right-sizing, Hetzner Volume billing model
- [ ] `internal/receipts` extended for Hetzner — Ed25519-signed receipt against Hetzner monthly invoice
- [ ] First GitLab design partner; first Hetzner design partner

### Differentiator additions (folded into Phase 8)
- [ ] `internal/agent/qa` + `internal/prwriter/thread` — `@optiqor` PR-thread Q&A. Engineers ask "@optiqor why did you suggest 6 GiB?" in the PR thread; the bot answers in-thread with the actual data points it used. Conversational AI in the PR layer (2 wk)
- [ ] `internal/receipts/currency` — multi-currency Receipts (EUR for Hetzner customers, GBP, etc.) signed against the original-currency invoice (3 days)

### Production-readiness — EU GA (Phase 8, gates GitLab + Hetzner customer onboarding)
- [ ] **Terraform `eu-west-1` deployment** — full Optiqor control plane in EU; replicates the prod stack
- [ ] **Region selection at signup** — user chooses US or EU; cannot change post-signup; tenant data never leaves region after first agent install
- [ ] **EU-specific Anthropic endpoint** — route all EU tenants through Anthropic's EU data-residency endpoint exclusively
- [ ] **EU-resident KMS Receipt-signing key** — separate KMS key in `eu-west-1`; EU Receipts signed by the EU key; transparency log shards per region
- [ ] EU subprocessor list updated; DPA template covers EU residency commitment

## Phase 9 — Months 9–12: Polish + Scale + Series A Posture

- [ ] Single tenant with mixed clusters (AWS + Azure + Hetzner) and mixed VCS (GitHub + GitLab) end-to-end
- [ ] Cost Spike + Auto-Rollback parity across all 3 clouds
- [ ] Hardening: chaos testing monthly, p95 PR/MR comment latency budgets enforced per VCS path
- [ ] SOC 2 Type 1 audit: fix gaps, close audit
- [ ] LLM cost optimization: prompt cache hit rate ≥ 50% (target 75% Year 2); per-cloud prompt templates

### Differentiator additions (folded into Phase 9)
- [ ] `internal/cost/regression` — cost regression detection (slow drift, not just spikes); separate alert path from Cost Spike (1 wk)
- [ ] `optiqor/detector-sdk` seed release — extract the 30 Year-1 detectors into the SDK shape and ship a rough public release; year ahead of original Y2 plan, builds community-contribution muscle (2 wk)

### Operator-Managed Workload Coverage — Layer 4 (Phase 9)
- [ ] Public `optiqor/operator-presets` repo (Apache 2.0) for community-contributed YAML presets
- [ ] CI validates preset schema and runs against a live cluster of the operator
- [ ] Optiqor reviews and merges; presets ship in next agent release; long tail of operators covered with zero per-operator engineering effort (3 days framework + ongoing review time)

### Operational backbone — full metrics + REST API + auditor mode (Phase 9)

#### Six-Metric Health Framework (full)
- [ ] `internal/metrics/retention` — gross retention (logo) + net revenue retention; cohort-based (start ARR + expansion − churn − contraction)
- [ ] `internal/metrics/churn` — logo + revenue churn per month
- [ ] `internal/metrics/expansion` — ARR growth from existing accounts (cluster adds, plan upgrades)
- [ ] `internal/metrics/health` — composite per-tenant score (PR merge rate · Receipt count last 30d · dashboard DAU · Slack engagement); 0–100 scale; alert at <50
- [ ] `internal/metrics/leading` — 7-day-ahead churn predictor (health score drop > 20 points in 7 days)
- [ ] `metrics.tenant_daily` TimescaleDB hypertable for nightly snapshots; cohort queries on top
- [ ] Customer-visible health score in dashboard ("your score: 78/100"); positive feedback loop
- [ ] PagerDuty integration for leading-churn alerts

#### REST API + TypeScript SDK
- [ ] `cmd/api` `/api/v1/...` routes generated from Go interfaces via oapi-codegen; OpenAPI 3.1 spec is the source of truth
- [ ] **Three-tier access control** in `internal/api/access`: Public (100 req/sec/token, plan-tier configurable, 99.9% SLA) · Partner (negotiated, 99.95% SLA) · Internal (unlimited, no SLA — eats own dogfood)
- [ ] API tokens scoped to tenant with scopes (`receipts:read`, `apply_fix:write`, `admin`); Argon2id hashed at rest; audit-logged
- [ ] OAuth 2.0 authorization-code flow for third-party app integrations (Backstage, Cortex, Port)
- [ ] Redis-backed rate limiter; `429` with proper `Retry-After`
- [ ] **`@optiqor/sdk-typescript`** (npm, MIT) — REST client + webhook signature verification
- [ ] `sandbox.optiqor.dev` — deterministic mock-data API for customer integration testing
- [ ] Docs site auto-generated from OpenAPI; CI fails if a public endpoint changes without docs update
- [ ] Every endpoint has runnable examples in `curl`, TypeScript, and Go (Go examples even though Go SDK is Y2)

#### Receipt Auditor Mode
- [ ] `internal/receipts/auditor` — third-party verification token signing flow; customer enters AWS account ID, we issue a one-time token
- [ ] External auditors use the token to query the customer's CUR independently and reconstruct the Receipt math; **we never share customer data with the auditor**
- [ ] Documented procurement / compliance flow with sample SOC-2-style attestation language

#### Public transparency (Month 6+, Phase 9 formalization)
- [ ] Quarterly transparency report blog post template
- [ ] Live status page at `status.optiqor.dev`: real-time per-tenant cost (anonymized) + SLO performance
- [ ] Open-source dogfooding Helm chart at `optiqor/dogfood` (lets customers install identical infra)

---

## Always-On

- [ ] Run `make lint test build` before pushing
- [ ] One ADR per non-trivial architectural decision
- [ ] No `panic()` in production code paths (errors are returned)
- [ ] Anything new touching customer data goes through tenant-scoped DB connections

---

## Archive (completed)

- [x] **Phase 0 (2026-04-25):** Day 0 scaffolding — repo layout, Go module, Cobra stubs, Dockerfiles, Terraform skeleton, CI workflow stubs.
