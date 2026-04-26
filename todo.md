# backend — Sprint Todo

Backend-scoped subset of the org-level [ROADMAP.md](ROADMAP.md). Update this as you ship; archive completed phases at the bottom.

> **Today: 2026-04-26.** Active phase: **Phase 1 — Foundation (Weeks 1–2).**
>
> **Year 1 surface (expanded):** AWS EKS · Azure AKS · Hetzner Cloud K8s · GitHub · GitLab · ArgoCD · Flux CD · Helm · Kustomize. Day 90 demo stays narrow (EKS + GitHub + ArgoCD + Helm); the rest lands in Phases 7-9 (Months 4-12).

## Phase 1 — Weeks 1–2: Foundation

### Infra (Terraform)
- [ ] `infra/terraform/modules/vpc` — VPC, subnets (public/private/db), NAT, IGW, flow logs
- [ ] `infra/terraform/modules/eks` — EKS 1.31 cluster, managed node groups, IAM roles for ServiceAccounts (IRSA)
- [ ] `infra/terraform/modules/rds-postgres` — RDS Postgres 16, parameter group enabling `pg_stat_statements` + TimescaleDB, automated backups, KMS encryption
- [ ] `infra/terraform/modules/elasticache` — Redis 7 cluster mode disabled (Year 1), encryption at-rest + in-transit
- [ ] `infra/terraform/modules/s3` — buckets for receipts, sandbox uploads, CUR ingest; per-tenant prefix policies
- [ ] `infra/terraform/modules/kms` — keys for RDS, S3, Secrets Manager, application-level (receipt signing key)
- [ ] `infra/terraform/modules/iam` — OIDC for GitHub Actions, ECR push role, ArgoCD sync role
- [ ] `infra/terraform/envs/dev/main.tf` — wire modules, remote state in S3 with DynamoDB lock
- [ ] `infra/terraform/envs/staging/main.tf`
- [ ] `infra/terraform/envs/prod/main.tf` (us-east-1 multi-AZ)

### App platform (`internal/platform/`)
- [ ] `config` — load from env + AWS Secrets Manager, validated with `go-playground/validator`
- [ ] `db` — pgx pool with tenant-scoped connection wrapper, `SET LOCAL app.tenant_id` on every checkout
- [ ] `db/redis` — go-redis client with `t:<tenant_id>:` prefix enforcement
- [ ] `logging` — slog handler that injects `tenant_id`/`request_id`/`workflow_id` from context
- [ ] `telemetry` — Prometheus metrics + OTel tracing setup; SDK init for both
- [ ] `featureflags` — OpenFeature client wired to Unleash

### Cross-cutting abstractions (Year 1 expansion enablers)
- [ ] `internal/billing/` — pluggable cost-source interface. AWS CUR is the first implementation in Phase 6; Azure Cost Management lands in Phase 7; Hetzner Cloud invoices land in Phase 8. The `Receipt` issuer takes `BillingSource` as an interface so cryptographic signing math stays uniform.
- [ ] `internal/vcs/` — pluggable source-control interface. GitHub is the first impl. GitLab slots in at Phase 8 with the same webhook → Temporal workflow surface, MR comment renderer, and signed-token Apply Fix flow.
- [ ] `internal/parser/gitops/` — sub-package per GitOps tool. ArgoCD Application reader at Phase 1; Flux `Kustomization` + `HelmRelease` reader at Phase 7. Both feed the same normalized internal representation.

### `cmd/api`
- [ ] HTTP server (chi or stdlib mux), graceful shutdown
- [ ] `/healthz` (liveness), `/readyz` (readiness — checks DB + Redis), `/metrics` (Prom)
- [ ] Middleware: request_id, tenant resolution from JWT, slog access log, pprof (auth-gated), panic recovery
- [ ] GitHub OAuth callback handler (login flow)
- [ ] GitHub App webhook receiver (signature verification, → Temporal workflow start)

### `cmd/worker`
- [ ] Temporal client + worker boot
- [ ] Worker registers per-tenant queue dynamically (controller pattern)
- [ ] One placeholder workflow + activity to validate end-to-end (will be replaced in Phase 2–3)

### `cmd/agent`
- [ ] Stub binary that prints version + connects to SaaS over mTLS, exits cleanly. Real watch loop ships Phase 5.

### CI/CD
- [ ] `.github/workflows/ci.yml` — golangci-lint + staticcheck + `go test -race ./...`
- [ ] `.github/workflows/security.yml` — gosec + govulncheck + trivy + gitleaks
- [ ] `.github/workflows/release.yml` — build → ECR (cosign-signed) → ArgoCD sync trigger
- [ ] `.github/workflows/codeql.yml`
- [ ] `.github/dependabot.yml`
- [ ] `.github/CODEOWNERS`

### Migrations
- [ ] goose setup, baseline migration with: `tenants`, `users`, `repos`, `prs`, `analyses`, `apply_fixes`, `receipts`, `llm_calls`
- [ ] RLS policies on all tenant-scoped tables
- [ ] Migration role separate from app role

### Observability
- [ ] Prometheus + Grafana + Loki + Tempo Helm charts in `deploy/helm/observability/`
- [ ] Sentry DSN configured per env
- [ ] SLO recording rules + alerts: API uptime, PR comment latency, cost/PR

### Cost visibility — tag from Day 1 (retrofit is expensive)
- [ ] Every AWS resource in Terraform tagged `Project=sevro Environment={dev,staging,prod} Tenant={shared|tenant-id}` via `default_tags` block
- [ ] Athena workgroup `sevro-cli-attribution` + named queries for `cost_per_tenant`, `cost_per_workflow`, `cost_per_environment`
- [ ] CI check: `terraform plan` fails if any resource is missing required tags

### Production-readiness baseline (Phase 1 — must land before Phase 2)

#### Multi-cluster + team hierarchy (architectural — retrofit later is expensive)
- [ ] Schema: `tenants → workspaces → clusters → namespaces → workloads` with FKs and RLS policies
- [ ] **Stable workload identity** — `workload_id = sha256(cluster_id || namespace || kind || canonical(primary_selector_labels))`; survives renames/recreations
- [ ] **Cross-cluster workload class group** — `workload_class_group_id` for fleet-wide recommendations
- [ ] Namespace-to-team mapping config per workspace; drives cost-attribution dashboards
- [ ] RLS policies extended with `workspace_id` for finer enterprise-customer scoping

#### Disaster Recovery foundations
- [ ] RDS Postgres: PITR enabled with 35-day retention (Terraform)
- [ ] RDS cross-region snapshot replication `us-east-1 → us-east-2` (daily)
- [ ] S3 Cross-Region Replication for `receipts/` and `sandbox/` buckets
- [ ] KMS keys for Receipt signing replicated to `us-east-2` (multi-region key alias)

#### GDPR baseline (legal must-have for any EU customer Y1)
- [ ] `internal/gdpr/dsar` — `GET /api/v1/dsar/export` (signed ZIP) and `DELETE /api/v1/dsar/erase` (30-day purge + tombstones)
- [ ] **Data retention enforcement** via Temporal cron — Prometheus snapshots: 90d · LLM call logs: 30d · Receipts: 7y · audit log: 7y
- [ ] DPA template + DocuSign integration; subprocessor list page with RSS feed for changes

**Exit:** `terraform apply` builds prod from scratch · tagged commit auto-deploys to staging via ArgoCD · `/healthz` returns 200 from prod · golangci-lint passes · `go test ./...` passes · `gitleaks` clean.

---

## Phase 2 — Weeks 3–4: Public Sandbox

- [ ] `internal/parser` — Helm values + templates parser
- [ ] `internal/sandbox` — public sandbox handlers
- [ ] `internal/cost` — sandbox-grade rule-based engine v0
- [ ] Frontend framework decision (Week 3 Day 1) → ADR
- [ ] `web/` — sandbox UI: paste textbox, results panel, ±40% accuracy banner, share button
- [ ] Shareable report URLs `/r/<hash>` (storage in S3)
- [ ] Rate limit middleware (Redis-backed, IP + fingerprint)
- [ ] p95 < 3s benchmark in CI (k6 or hey)

---

## Phase 3 — Weeks 5–6: Detectors + LLM Diff + CLI v0.1

- [ ] 15 cost detectors in `internal/cost/detectors/`
- [ ] 15 security detectors in `internal/cost/detectors/sec/` (TODO: rename to `internal/security/`)
- [ ] `internal/confidence` — Low/Med/High banding
- [ ] `internal/agent/llm` — Anthropic SDK wrapper with prompt caching
- [ ] `internal/agent/budget` — $0.40/analysis cap
- [ ] LLM call accounting → `llm_calls` table
- [ ] CLI: `cli/` Phase 3 work happens in the other repo. Backend ships nothing CLI-related except the share-URL upload endpoint.

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
