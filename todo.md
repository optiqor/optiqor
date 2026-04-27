# backend — Sprint Todo

Backend-scoped subset of the org-level [ROADMAP.md](ROADMAP.md). This file is the **canonical engineering tracker for backend work**; CLI-side phase work lives in the [sevro repo](https://github.com/lowplane/sevro). The cross-repo Phase view (cost-detector breakdowns, CLI runtime status, etc.) lives in [ROADMAP.md](ROADMAP.md) — keep both files in sync when a phase milestone moves.

> **Today: 2026-04-27.** Active phase: **Phase 1 — Foundation (Weeks 1–2).**
>
> **Year 1 surface (expanded):** AWS EKS · Azure AKS · Hetzner Cloud K8s · GitHub · GitLab · ArgoCD · Flux CD · Helm · Kustomize. Day 90 demo stays narrow (EKS + GitHub + ArgoCD + Helm); the rest lands in Phases 7-9 (Months 4-12).
>
> **Cross-repo split (consistent with [ROADMAP.md](ROADMAP.md)):**
> - **This repo (backend)** — proprietary monorepo: API server, Temporal worker, in-cluster K8s agent, sandbox web frontend, Terraform infra, Receipt issuer, LLM Apply Fix path
> - **[lowplane/sevro](https://github.com/lowplane/sevro)** — Apache-2.0 OSS CLI: deterministic 30-detector rule engine, `analyze`/`demo`/`diff`/`score`/`audit`/`compare`, `--share` HTTPS upload, `@sevro/cli` npm package

## Phase 1 — Weeks 1–2: Foundation

> **Status (2026-04-27):** Phase 1 **code + infra-as-code surface is complete**. The remaining `[ ]` items below all require an AWS account and live infrastructure (`terraform apply`, EKS bootstrap, ArgoCD install). They ship in the first sprint after pre-seed funding binds the AWS account; the Terraform code itself is committed and `terraform fmt`-clean.

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
- [x] Middleware: request_id, tenant resolution from `X-Sevro-Tenant` header (Phase 1 dev surface), slog access log, panic recovery
- [x] GitHub App webhook receiver — HMAC verification + 8MiB body cap + 202 ack
- [x] GitHub OAuth callback handler (Phase 1 ack stub — session issuance lands in Phase 5)
- [x] pprof endpoints behind `SEVRO_ADMIN_TOKEN` constant-time check (disabled when token is empty)
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
- [x] Migration role separate from app role (`sevro_migrator NOLOGIN BYPASSRLS` vs `sevro_app NOLOGIN`)

### Observability (code + Helm complete; cluster install pending EKS)
- [x] Prometheus + Grafana + Loki + Tempo + OTel Collector Helm values in `deploy/helm/observability/`
- [x] SLO recording rules + alerts in `deploy/helm/observability/rules/sevro-slo.yaml` (API uptime ≥ 99.5%, PR comment p95 < 45s, sandbox p95 < 3s, cost/PR < $0.40, Apply Fix success > 85%)
- [x] Sentry init shim in `internal/platform/telemetry` (no-op default; `NewSentryReporter` adapter ships in Phase 5)
- [ ] `helm install` of the observability stack on the live EKS cluster

### Cost visibility — tag from Day 1
- [x] Every env's Terraform `provider "aws"` block declares `default_tags` with `Project`, `Environment`, `Tenant`, `ManagedBy`
- [x] CI check (`scripts/check-terraform-tags.sh`) fails if any env file is missing the required tag keys; wired into `.github/workflows/ci.yml`
- [x] `terraform fmt -recursive -check` runs in CI
- [ ] Athena workgroup `sevro-cost-attribution` + named queries for `cost_per_tenant`, `cost_per_workflow`, `cost_per_environment` (lands with first prod CUR ingest in Phase 6)

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

> **Backend scope only.** CLI-side parser, cost engine, shareable-URL hashing, and the `--share` upload client all ship in the [sevro repo](https://github.com/lowplane/sevro); see [ROADMAP.md](ROADMAP.md) for the cross-repo view. The backend Phase 2 work is the sandbox **receiver** — the public HTTP endpoint that accepts uploaded analyses, deduplicates them by hash, and renders a stable share URL.

- [ ] `internal/parser` — Helm values + templates parser. **Values normalisation is reused from `github.com/lowplane/sevro/pkg/parser`** (single source of truth — same `Workload` struct the CLI's detectors run against). This package owns only what the SaaS needs beyond static values: rendered-template parsing, Kustomize overlays, ArgoCD `Application`/Flux `HelmRelease` resolution, and the multi-source bundling for sandbox uploads
- [ ] `internal/sandbox/handlers` — public sandbox API: `POST /api/v1/share` (accepts `X-Sevro-Hash`-headered upload), `GET /r/{hash}` (HTML render), `GET /api/v1/r/{hash}` (JSON)
- [ ] `internal/cost` — sandbox-grade rule-based engine v0 (thin wrapper around `github.com/lowplane/sevro/pkg/rules`; server-issued Receipts and shareable analyses cite the exact same `DetectorID`s the CLI does — no fork)
- [ ] Frontend framework decision (Week 3 Day 1) → ADR — Next.js / Remix / Vite+React
- [ ] `web/` — sandbox UI: paste textbox, results panel, ±40% accuracy banner, share button
- [ ] Shareable report storage — S3 bucket `sevro-prod-sandbox` already provisioned in Phase 1 Terraform with KMS + 30-day lifecycle + CRR; receiver writes content-addressed objects keyed by the SHA-256 the CLI sends in `X-Sevro-Hash`
- [ ] Rate limit middleware (Redis-backed, IP + fingerprint) — wired into `cmd/api` via the existing `internal/platform/db/redis` Keyspace
- [ ] p95 < 3s benchmark in CI (k6 or hey)

**CLI side already shipped (see [sevro repo](https://github.com/lowplane/sevro)):**
- [x] Helm values parser, 30-detector engine, shareable-URL hashing, `--share` HTTPS upload client with graceful offline fallback

---

## Phase 3 — Weeks 5–6: Detectors + LLM Diff + CLI v0.1

> **Backend scope is the LLM-driven Apply Fix path.** The deterministic 30-detector library is the CLI's responsibility (per the OSS playbook hard rule: no LLM in the CLI); the backend imports/mirrors the same rule definitions so server-issued recommendations cite the exact same detector IDs.

- [x] **30 detectors mirrored server-side** — canonical implementations live in the CLI's public `pkg/rules` library and are imported directly via `go.mod` (`github.com/lowplane/sevro/pkg/rules` + `github.com/lowplane/sevro/pkg/parser`). No fork, no duplication — backend's `internal/cost` calls `rules.Run(workloads, rules.All())` against the same struct types the CLI emits. New detectors land in the CLI's `pkg/rules` first; a `go get -u github.com/lowplane/sevro` in the backend picks them up automatically. Golden parity tests in `tests/integration/cli_parity_test.go` assert the CLI binary and the backend produce the same `Finding` set for the canonical fixtures
  - Source: 15 cost + 15 security detectors, CIS Kubernetes Benchmark / NSA hardening guide aligned (see [ROADMAP.md](ROADMAP.md) Phase 3 detector tables)
- [ ] `internal/confidence` — Low/Med/High banding (server-side helper that the LLM augmentation layer down-ranks based on validator-rejection signals; CLI has the qualitative-only equivalent)
- [ ] `internal/agent/llm` — Anthropic SDK wrapper with prompt caching (50 % hit-rate target Year 1)
- [ ] `internal/agent/budget` — $0.40/analysis cap, enforced before every Sonnet/Opus call
- [ ] LLM call accounting → `llm_calls` table (already provisioned in `migrations/0001_baseline.sql`)
- [ ] LLM-generated Apply Fix diff renderer (consumes detector findings + measured Prometheus context, emits Helm values diff)
- [ ] LLM canary: same prompt occasionally sent to Sonnet AND Haiku; outputs compared, divergence alerts (todo.md production-readiness gap #6 Layer 3)

**CLI side already shipped (see [sevro repo](https://github.com/lowplane/sevro)):**
- [x] 15 cost detectors + 15 security detectors firing on bundled demo (30/30)
- [x] Confidence band engine (`internal/rules.Confidence`)
- [x] CLI v0.1 build + `npm pack` proven (14 KB `@sevro/cli` tarball); `release.yml` workflow drives publish on `v*` tag
- [x] Commands: `analyze`, `demo`, `diff`, `score`, `audit`, `compare` + `--version`/`--help`
- [x] ASCII + JSON output with mandatory ±40% accuracy disclosure
- [x] `--share` opt-in upload to `https://sandbox.sevro.dev/api/v1/share` over HTTPS with 5 s timeout and graceful offline fallback (overridable via `SEVRO_SHARE_URL`)

---

## Phase 4 — Weeks 7–8: PR Writer + Apply Fix

- [ ] `internal/prwriter` — PR comment markdown renderer + Apply Fix flow
- [ ] Signed-token Apply Fix endpoint
- [ ] PR comment latency p95 < 30s (instrument every step)
- [ ] Skeptic Mode toggle
- [ ] `internal/operators/detector` — Layer 1 of operator-coverage engine: walk `ownerReferences` on every Deployment / StatefulSet / DaemonSet; classify as `direct` or `operator:<group/kind>`; gate Apply Fix dispatch on result. Replaces "skip with explanation" floor; full four-layer design lifts effective coverage from ~60% → ~98% (~2 days)

### Tier-1 data sources (gate Apply Fix — must precede Phase 5 onboarding)
- [ ] `internal/agent/k8s/events` — K8s Events stream ingestion (1 wk)
- [ ] `internal/agent/k8s/hpa` — HPA state reader, normalize `minReplicas`/`maxReplicas`/current target (1 wk)
- [ ] `internal/agent/k8s/policy` — PDB + ResourceQuota + LimitRange constraint readers (1 wk)
- [ ] `internal/cost/oomkilled` — OOMKilled history scrape from Prometheus, 7-day window (3 days)

### Algorithmic improvement: Validation Before Recommendation (1 wk)
- [ ] New package `internal/validator/` — interface `Validate(ctx, *tenancy.Context, candidate) (Result, error)`
- [ ] Validators: `pdb`, `resourcequota`, `limitrange`, `hpabounds`, `dependency`, `oom-recent`
- [ ] Wire as a pipeline stage between `internal/cost` (candidate generation) and `internal/prwriter` (rendering)
- [ ] Rejected candidates logged with reason for tuning the detector library (not surfaced to PR comment)
- [ ] Metric: `sevro_validator_rejects_total{reason}`; alert if reject rate jumps >2× week-over-week (signals a detector regression)

### Differentiator additions (folded into Phase 4)
- [ ] `internal/prwriter/narrative` — LLM-generated 2-sentence diff narrative at the top of every PR comment (3 days)
- [ ] `internal/cost/detectors/sec/cis` — CIS Kubernetes Benchmark control IDs attached to each security finding (2 days)
- [ ] `internal/prwriter/labels` — PR labels-as-policy parser (`sevro:skip`, `sevro:budget=$X`, `sevro:wait-for-prom=Nd`) (2 days)
- [ ] `internal/ingestion/coalesce` — collapse two PRs against the same chart within 24h into one analysis (2 days)

### Production-readiness — Apply Fix safety + LLM defense + environment classification (Phase 4)

#### Pre-merge validation gate (gates Apply Fix dispatch — must precede Phase 5)
- [ ] `internal/applyfix/gate/render` — sandboxed `helm template` / `kustomize build`; fails if templating breaks (3 days)
- [ ] `internal/applyfix/gate/conform` — `kubeconform` against the cluster's actual API version
- [ ] `internal/applyfix/gate/dryrun` — agent-side `kubectl --dry-run=server`; signed-token round-trip; catches custom admission webhooks (Kyverno, Gatekeeper, OPA) the validator can't predict (1.5 wk)

#### Prompt injection defense + LLM output validation
- [ ] `internal/agent/llm/sanitizer` — strip Helm template comments; detect injection patterns (`ignore previous`, `system:`, etc.) and wrap suspicious content in `<USER_DATA>` boundaries with explicit "never trust" instructions; per-field length limits (3 days)
- [ ] `internal/agent/llm/validator` — every LLM-generated YAML diff runs the same render/conform/dryrun gate; schema-aware sanity checks (within ±10× of current values); confidence-down-rank for borderline outputs (1 wk)
- [ ] `internal/agent/llm/audit` — per-prompt audit log (input hash, output hash, model, cost) for forensic trail
- [ ] `internal/agent/llm/canary` — same prompt occasionally sent to Sonnet AND Haiku; outputs compared; divergence alerts on hallucination patterns

#### Environment classification (input to safety profiles)
- [ ] `internal/safety/environment` — detect `prod` / `staging` / `dev` / `unknown` from labels (`environment=prod`), namespace patterns (`*-prod`, `production-*`), or customer-configured rules; **default to `prod` (fail-safe)** when unknown
- [ ] Per-environment aggressiveness in `internal/cost/strategy`:
  - `prod`: P99 sizing, no memory cuts >10%, no replica reductions in single PR, **High-confidence-only** Apply Fix, manual approval required
  - `staging`: P95 sizing, memory cuts up to 25%, Medium+ confidence
  - `dev`: P95 sizing, full range, Low+ confidence, auto-merge eligible (opt-in)

---

## Phase 5 — Weeks 9–10: Design Partner #1 + Slack + Dashboards

- [ ] `cmd/agent` real watch loop: client-go informers + Prometheus scrape, mTLS to SaaS
- [ ] Helm chart in `deploy/helm/sevro-agent/` for customer install
- [ ] Slack: digest workflow, `/sevro status` slash command
- [ ] Customer dashboard pages in `web/`
- [ ] On-call docs + runbooks in `docs/runbooks/`

### Tier-1 data sources (agent-resident — round out the data picture)
- [ ] `internal/agent/k8s/topology` — Service / Endpoints graph (workload→service→endpoint), exposed to backend for "no live traffic" detection (2 wk)
- [ ] `internal/agent/nodeprov/` — three-tier `NodeProvisioner` adapter (replaces single `internal/agent/karpenter`); detect at agent install via pre-flight, store class on `tenants.node_provisioner_class`:
  - `nodeprov/karpenter` — T1: NodePool + NodeClaim resource reader; high-confidence node math (1 wk)
  - `nodeprov/autoscaler` — T2: detect `cluster-autoscaler` Deployment; read ASG configs via AWS API; infer instance shapes from ASG min/max + node labels; medium-confidence (1 wk)
  - `nodeprov/static` — T3: no autoscaler detected; render manual-step recommendations; cap confidence at Medium (3 days)
  - `nodeprov/managed/aks` — Phase 7 (AKS node pools)
  - `nodeprov/managed/hetzner` — Phase 8 (Hetzner Cloud node pools)
- [ ] `internal/cost/strategy` — node-provisioner-class is an input; sizing strategies vary by tier (Karpenter can recommend rapid scale; static node groups cannot)

### Differentiator additions (folded into Phase 5)
- [ ] `internal/notify/slack/diff` — render Apply Fix diff inline in Slack thread for mobile-first review (3 days)
- [ ] `internal/integrations/argocd-notifications` — accept ArgoCD Notifications webhook back into our pipeline; close the Apply Fix → merge → sync → measure → Receipt loop (3 days)

### Operational backbone — onboarding + sevro-on-sevro (Phase 5)
- [ ] `internal/onboarding/` — state machine (`signed_up → vcs_connected → repo_selected → first_pr_analyzed → agent_installed → first_apply_fix → first_receipt_issued`); each transition timestamped in `tenants.onboarding_state` JSONB column
- [ ] `internal/onboarding/preflight` — pre-flight checker reads cluster K8s version, Prometheus presence, RBAC, Karpenter, PDB/RQ counts; renders preview page before `helm install`
- [ ] `internal/onboarding/nudges` — Temporal cron workflows: 24h no-VCS email · 72h no-agent in-app prompt · 7-day no-Apply-Fix CSM/Slack alert
- [ ] `internal/onboarding/demo` — synthetic-but-clearly-labeled demo data path for clusters with <30 days of Prometheus history
- [ ] `cmd/api` route `/onboarding/health` — per-tenant funnel position + blockers; shareable with the customer
- [ ] **Sevro-on-Sevro install** against our own EKS — production GitHub App, in-cluster agent on `prod` cluster, every PR to `backend/` gets a Sevro comment (zero engineering effort beyond using the product)
- [ ] `internal/metrics/activation` — Activation Rate (≥60% target) + Time to First Receipt (≤35 days p50) computed daily

### Hard SLOs to enforce in Phase 5
- [ ] Sandbox p95 < 3s (already in Phase 2, re-verify)
- [ ] App install → first PR/MR comment < 10 min p95
- [ ] Agent install → first recommendation < 30 min p95
- [ ] Agent install → first Cloud Receipt < 35 days p50

### Production-readiness — recommendation lifecycle + blast-radius (Phase 5)

#### Recommendation lifecycle (drives churn if missing)
- [ ] `internal/recommendations/lifecycle` — five-state machine: `active` / `snoozed` / `dismissed` / `ignored-workload` / `ignored-class`
- [ ] PR-label parser handles `sevro:snooze=14d` (label-driven snooze)
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

- [ ] AWS CUR ingest workflow (Athena query, daily partition) — first concrete `internal/billing/aws` impl
- [ ] `internal/receipts` — Ed25519 signing + verification endpoint (public)
- [ ] Cost Spike detector workflow (anomaly → most-likely-PR mapping)
- [ ] `internal/rollback` — 7-day post-merge metric monitor + auto-rollback PR

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
- [ ] **`@sevro/verify` CLI** (npm, Apache 2.0, lives in `cli/` repo) — fetches public key via DID/HKP, validates Ed25519 offline, exit code 0/1 for CI gating
- [ ] Yearly key rotation procedure documented; old keys remain valid forever via tlog
- [ ] Documented + pen-tested compromise procedure (revoke in tlog → rolling-shadow re-sign → 24h customer alert)
- [ ] Stable Receipt YAML schema versioned at `methodology.sevro.dev/<methodology>/<version>`

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
- [ ] `internal/billing/meter` — usage metering reports cluster-count, workload-count, Receipt-count to Stripe Billing on the right axis per plan
- [ ] `internal/platform/plans` — plan-limits enforcement (Free: 2 clusters / 1 Receipt-per-month · Team $500/mo: 5 clusters / unlimited · Enterprise: custom)
- [ ] **14-day trial flow** for Team tier with Temporal-driven nudges at T-3 / T-1 / expiry; auto-downgrade to Free at expiry
- [ ] Annual billing with 15% discount; auto-invoicing for Enterprise contracts
- [ ] Stripe Tax integration for EU/UK/AU VAT (required for Hetzner customers)
- [ ] Plan-change webhooks (`subscription.upgraded`, `subscription.downgraded`, `trial.ended`) emitted via existing `internal/api/webhooks`

#### Disaster Recovery drills (SOC2 evidence)
- [ ] `cmd/worker` Temporal cron: monthly automated restore drill — restore last night's snapshot to scratch RDS, run schema integrity check, report time-to-restore; oncall paged on failure
- [ ] **Backup integrity** — every snapshot's hash signed with the same KMS key family as Receipts; integrity verifiable independently of AWS
- [ ] `backend/docs/runbooks/disaster-recovery.md` — step-by-step recovery procedure; first fire drill executed and timed
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
- [ ] Metric: `sevro_recommendation_accuracy_by_class` — weekly accuracy lift dashboard

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
- [ ] Coverage SLO instrumented: `sevro_workload_coverage_ratio` per tenant; alert if a tenant's ratio < 90% (signals a missing preset for an operator they're using heavily)

## Phase 8 — Months 6–9: GitLab + Hetzner Cloud K8s

- [ ] `internal/vcs/gitlab` — OAuth, webhook receiver, MR comment renderer, signed-token Apply Fix → MR opener
- [ ] GitLab CI/CD integration parity with GitHub Actions Marketplace listing
- [ ] `internal/billing/hetzner` — Hetzner Cloud invoice connector (flat-rate per-server math)
- [ ] Hetzner-specific detectors: dedicated-vCPU (CCX) vs shared-vCPU (CX) right-sizing, Hetzner Volume billing model
- [ ] `internal/receipts` extended for Hetzner — Ed25519-signed receipt against Hetzner monthly invoice
- [ ] First GitLab design partner; first Hetzner design partner

### Differentiator additions (folded into Phase 8)
- [ ] `internal/agent/qa` + `internal/prwriter/thread` — `@sevro` PR-thread Q&A. Engineers ask "@sevro why did you suggest 6 GiB?" in the PR thread; the bot answers in-thread with the actual data points it used. Conversational AI in the PR layer (2 wk)
- [ ] `internal/receipts/currency` — multi-currency Receipts (EUR for Hetzner customers, GBP, etc.) signed against the original-currency invoice (3 days)

### Production-readiness — EU GA (Phase 8, gates GitLab + Hetzner customer onboarding)
- [ ] **Terraform `eu-west-1` deployment** — full Sevro control plane in EU; replicates the prod stack
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
- [ ] `sevro/detector-sdk` seed release — extract the 30 Year-1 detectors into the SDK shape and ship a rough public release; year ahead of original Y2 plan, builds community-contribution muscle (2 wk)

### Operator-Managed Workload Coverage — Layer 4 (Phase 9)
- [ ] Public `sevro/operator-presets` repo (Apache 2.0) for community-contributed YAML presets
- [ ] CI validates preset schema and runs against a live cluster of the operator
- [ ] Sevro reviews and merges; presets ship in next agent release; long tail of operators covered with zero per-operator engineering effort (3 days framework + ongoing review time)

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
- [ ] **`@sevro/sdk-typescript`** (npm, MIT) — REST client + webhook signature verification
- [ ] `sandbox.sevro.dev` — deterministic mock-data API for customer integration testing
- [ ] Docs site auto-generated from OpenAPI; CI fails if a public endpoint changes without docs update
- [ ] Every endpoint has runnable examples in `curl`, TypeScript, and Go (Go examples even though Go SDK is Y2)

#### Receipt Auditor Mode
- [ ] `internal/receipts/auditor` — third-party verification token signing flow; customer enters AWS account ID, we issue a one-time token
- [ ] External auditors use the token to query the customer's CUR independently and reconstruct the Receipt math; **we never share customer data with the auditor**
- [ ] Documented procurement / compliance flow with sample SOC-2-style attestation language

#### Public transparency (Month 6+, Phase 9 formalization)
- [ ] Quarterly transparency report blog post template
- [ ] Live status page at `status.sevro.dev`: real-time per-tenant cost (anonymized) + SLO performance
- [ ] Open-source dogfooding Helm chart at `sevro/dogfood` (lets customers install identical infra)

---

## Always-On

- [ ] Run `make lint test build` before pushing
- [ ] One ADR per non-trivial architectural decision
- [ ] No `panic()` in production code paths (errors are returned)
- [ ] Anything new touching customer data goes through tenant-scoped DB connections

---

## Archive (completed)

- [x] **Phase 0 (2026-04-25):** Day 0 scaffolding — repo layout, Go module, Cobra stubs, Dockerfiles, Terraform skeleton, CI workflow stubs.
