# Optiqor — Technical Implementation

> **Kubernetes-first, modular-monolith, boring-tech. This is how we actually build it.**

> ## Amendments — 2026-04-26
>
> The original draft assumed an EKS-only Year 1 with a single `internal/cost` engine reading AWS CUR. After review, the architecture has been generalized to support the expanded Year 1 surface (AKS + Hetzner + GitLab + Flux) without rewriting the foundation later.
>
> **New cross-cutting abstractions** (added to Phase 1, before any business logic):
>
> - **`internal/billing/`** — pluggable cost-source interface. AWS CUR is one impl; Azure Cost Management lands Phase 7; Hetzner Cloud invoices land Phase 8; GCP BigQuery in Year 2. The Receipt issuer takes `BillingSource` as an interface so Ed25519 signing is uniform across tiers.
> - **`internal/vcs/`** — pluggable source-control interface. GitHub first; GitLab second (Phase 8). Webhook receivers, PR/MR APIs, signed-token Apply Fix all hide behind one interface.
> - **`internal/parser/gitops/`** — sub-package per GitOps tool. ArgoCD reader Phase 1; Flux `Kustomization` + `HelmRelease` reader Phase 7. Both feed the same normalized representation.
>
> **Three-tier Receipt model** (replaces "Receipt = CUR-verified dollar figure"):
>
> | Tier | Cluster type | What's signed | Math |
> | --- | --- | --- | --- |
> | Cloud Receipt | EKS / AKS / GKE | "$X verified against cloud bill" | bill-line-item delta |
> | Capacity Receipt | Hetzner / on-prem / bare-metal | "freed N cores + M GiB, deferring $Y in next hardware spend" | utilization delta × CapEx amortization |
> | Hybrid Receipt | Mixed-cloud single tenant | sum across clusters with per-tier breakdown | aggregation of the above, signed once |
>
> All three use `internal/receipts` Ed25519 signing; the public verification endpoint accepts all three tiers.
>
> **Tier-1 data sources** added to the agent (Phase 4–5): K8s Events stream · HPA state · PDB / RQ / LimitRange constraint readers · OOMKilled history (Prometheus) · Service/Endpoints topology graph · Karpenter NodePool integration. The agent's data manifest is the universal-K8s-coverage promise — every conformant cluster ships these regardless of cloud.
>
> **Two algorithmic additions:**
>
> - **`internal/validator/`** (Phase 4, ~1 wk) — pipeline stage between candidate generation (`internal/cost`) and PR rendering (`internal/prwriter`). Validators: `pdb`, `resourcequota`, `limitrange`, `hpabounds`, `dependency`, `oom-recent`. Rejects ~5–10% of impossible recommendations with logged reasons. Metric: `optiqor_validator_rejects_total{reason}`.
> - **`internal/workload/classifier/`** (Phase 7, ~3 wk) — partitions workloads into `web-steady` / `worker-bursty` / `batch` / `stateful-db` / `ml-inference` / `unknown`. Per-class strategies in `internal/cost/strategy/`. Backfill via Temporal workflow.
>
> **Universal agent positioning.** Even though Year 1 only ships EKS / AKS / Hetzner billing connectors, the agent itself runs on any CNCF-conformant K8s from Day 1. The CLI runs against any K8s. Marketing positioning at Show HN (Month 3) is "any K8s, anywhere" — Receipts are explicitly Tier-1 cloud-only at launch and that's stated honestly.
>
> **Team & budget revision:** 3-engineer team grows to 5 by Month 4 (Engineers #4 and #5 hired earlier than original plan). Seed target $12–15M. Original 3-person / 12-week / $1.5M-pre-seed framing below remains accurate for the Day 90 canonical demo only.
>
> **Operational backbone — five new internal subsystems** added so we don't ship a leaky bucket:
>
> 1. **`internal/onboarding/`** — three-tier onboarding state machine (`signed_up → vcs_connected → repo_selected → first_pr_analyzed → agent_installed → first_apply_fix → first_receipt_issued`) with Temporal-driven nudges, pre-flight cluster checks before `helm install`, and synthetic-but-labeled demo data fallback for clusters with <30 days of Prometheus history. Hard SLOs: Sandbox p95 < 3s · install → first comment < 10 min · install → first recommendation < 30 min · install → first Receipt < 35 days. Phase 5 foundations, Phase 9 full self-serve.
>
> 2. **`internal/receipts/{signing,tlog,auditor}`** — three-surface verification:
>    - Public verification page with browser WebCrypto verification (no server roundtrip)
>    - `@optiqor/verify` CLI (Apache 2.0, npm) with offline operation for air-gapped enterprise
>    - Auditor mode: third-party verification token lets external auditors query the customer's CUR independently — we never aggregate customer billing data
>    - **AWS KMS asymmetric `SIGN_VERIFY` keys** (no extraction, sign-inside-HSM)
>    - **Sigstore Rekor-style transparency log** — must ship with the first Receipt because Merkle history can't be retrofitted
>    - Stable receipt YAML schema versioned at `methodology.optiqor.dev/<methodology>/<version>`
>    - Phase 6 for verification page + CLI + tlog + KMS; Phase 9 for auditor mode.
>
> 3. **`internal/metrics/{activation,retention,churn,expansion,health,leading}`** — six-metric health framework computed nightly into `metrics.tenant_daily` (TimescaleDB hypertable). Activation Rate (≥60% Y1) · Time to First Receipt (≤35 days p50) · Gross Retention (≥95%) · NRR (≥120%) · per-tenant Health Score (0–100) · Leading Churn Indicator. Customer-visible health score in dashboard creates positive feedback loops. Phase 5 lights up activation; Phase 9 ships the full framework.
>
> 4. **`internal/api/` + `cmd/api/v1/...` + webhooks** — REST API (OpenAPI 3.1, oapi-codegen, `/v1/` forever stable), webhooks (`receipt.issued`, `apply_fix.merged`, `cost_spike.detected`, `rollback.opened`, `validator.rejected`, `health_score.changed` — HMAC-SHA256, 30-day replay buffer), and `@optiqor/sdk-typescript` (npm, MIT). Three-tier access (Public / Partner / Internal) with different rate limits + SLAs. **GraphQL deferred until customers ask** — don't pre-build query languages for use cases nobody has demanded. **Go SDK deferred to Year 2.** Webhooks Phase 6 (cheap, high-value); REST + TS SDK Phase 9.
>
> 5. **Cost visibility — Optiqor-on-Optiqor + cost-attribution dashboard** — install our own product against our own EKS from Day 1 of Phase 5; Terraform `default_tags` from Phase 1 (`Project=optiqor Environment={dev,staging,prod} Tenant={shared|tenant-id}`); CI fails if any TF resource is missing required tags; Athena workgroup `optiqor-cli-attribution` powers daily Grafana panels (`cost_per_tenant`, `cost_per_workflow`, `cost_per_apply_fix`, `cost_per_llm_call{model}`, `cost_per_receipt`); monthly Cost Council; LLM cost guardrails (per-tenant budget, prompt cache-hit-rate tracking, weekly top-10 expensive prompt review); idle-resource auto-shutdown (staging scales to zero overnight + weekends). **Public quarterly transparency report from Month 6.**
>
> Total operational-backbone effort: **~21 engineer-weeks** distributed across Phases 1, 5, 6, 9.
>
> **Two coverage gaps closed** (added 2026-04-26 review):
>
> 1. **Node-Provisioner Adapter** (`internal/agent/nodeprov/`, replaces single Karpenter integration): three-tier abstraction — T1 Karpenter · T2 Cluster Autoscaler + ASG · T3 static node groups · plus managed-cloud adapters for AKS (Phase 7) and Hetzner (Phase 8). Detection at agent install; `tenants.node_provisioner_class` stored. Every recommendation carries provisioner context. Workload classifier factors provisioner tier into sizing strategy. **+1 wk over original Karpenter-only plan; covers the ~70% of EKS shops that don't run Karpenter.**
>
> 2. **Four-layer Operator Coverage Engine** (`internal/operators/{detector,schema,advice,presets}`, replaces "skip with explanation" floor):
>    - L1 owner-reference walker (Phase 4, 2 days) — 100% accurate operator-ownership detection
>    - L2 generic CRD-aware advice (Phase 7, 1 wk) — copy-pasteable YAML patch for any operator with a CRD OpenAPI schema
>    - L3 top-5 presets (Phase 7, 1 wk) — Prometheus Operator, kube-prometheus-stack, cert-manager, Strimzi, Istio
>    - L4 community-contributed presets (Phase 9, 3 days framework) — `optiqor/operator-presets` Apache-2.0 repo
>
>    **Effective Y1 workload coverage rises from ~60% → ~98%** for ~3 weeks of work. Coverage SLO `optiqor_workload_coverage_ratio` per tenant; alert at <90%.
>
> **Eight production-readiness gaps closed** (added 2026-04-26 review, total ~19.5 engineer-weeks distributed across Phases 1, 4, 5, 6, 8):
>
> 1. **Apply Fix Pre-Merge Validation Gate** (`internal/applyfix/gate/{render,conform,dryrun}`, Phase 4, ~2 wk) — 3-stage gate: helm-template/kustomize-build, kubeconform against actual K8s API version, and agent-side `kubectl --dry-run=server` (catches custom admission webhooks like Kyverno/Gatekeeper/OPA the validator can't predict). Same infrastructure also gates LLM output.
>
> 2. **Recommendation Lifecycle Management** (`internal/recommendations/lifecycle`, Phase 5, ~2 wk) — five-state machine (`active` / `snoozed` / `dismissed` / `ignored-workload` / `ignored-class`); PR-label parser; drift detection (agent compares cluster state and routes manually-applied changes through measured-delta → Receipt path); 60-day expiration; re-emergence on evidence change only. Without this, PR comments become noise within 2 weeks of install — single biggest churn driver.
>
> 3. **Multi-Cluster + Team/Namespace Hierarchy** (Phase 1, ~2 wk — architectural) — schema locked in Phase 1: `tenants → workspaces → clusters → namespaces → workloads`. Stable workload identity = `sha256(cluster_id || namespace || kind || canonical(primary_selector_labels))` (survives renames). Cross-cluster `workload_class_group_id` for fleet-wide recommendations. RLS extended with `workspace_id`. Trivial now; awful migration after 50 customers.
>
> 4. **Stripe Billing Infrastructure** (`internal/billing/stripe` + `internal/billing/meter` + `internal/platform/plans`, Phase 6, ~3 wk) — subscription lifecycle, customer portal, usage metering, plan limits, 14-day trial with Temporal-driven nudges, annual billing with 15% discount, Stripe Tax for EU VAT. Plan-change webhooks emitted via existing `internal/api/webhooks`.
>
> 5. **GDPR + EU Data Residency** (Phase 1 baseline + Phase 8 GA, ~5 wk total) — DPA template, public subprocessor list with RSS feed, DSAR endpoints (`export` + `erase` with 30-day purge + tombstones), enforced data retention (Prometheus 90d / LLM logs 30d / Receipts 7y / audit log 7y), PII minimization in LLM prompts (`internal/agent/llm/sanitizer`). Phase 8: full `eu-west-1` Terraform deployment, region selection at signup (immutable post-signup), Anthropic EU-residency endpoint exclusively for EU tenants, EU-resident KMS Receipt-signing key.
>
> 6. **Prompt Injection Defense + LLM Output Validation** (`internal/agent/llm/{sanitizer,validator,audit,canary}`, Phase 4, ~2 wk) — input sanitization (strip Helm comments, detect injection patterns, wrap suspicious content in `<USER_DATA>` boundaries with explicit "never trust" instructions, per-field length limits), output validation through the same render/conform/dryrun gate as Apply Fix, schema-aware sanity (within ±10× of current values), per-prompt audit log, Sonnet/Haiku canary divergence detection.
>
> 7. **Per-Environment Safety Profiles + Blast-Radius Scoring** (`internal/safety/{environment,blastradius}`, Phase 4 + 5, ~1.5 wk) — environment classification (`prod` / `staging` / `dev` / `unknown`); **`unknown` defaults to `prod` (fail-safe)**. Per-environment aggressiveness in `internal/cost/strategy`: prod uses P99 sizing with no >10% memory cuts and no replica reductions in single PR; staging uses P95 with up to 25% memory cuts; dev uses full range. Blast-radius score 1–5 walks Service/Endpoints topology graph; score 5 disables Apply Fix entirely and surfaces caveat-only recommendation.
>
> 8. **Disaster Recovery + Backup Strategy** (Phase 1 + 6, ~2 wk) — RDS PITR (35-day retention), cross-region snapshot replication (`us-east-1 → us-east-2`), S3 CRR on Receipt + sandbox buckets, multi-region KMS keys for Receipt signing, monthly automated restore drills via Temporal cron (oncall paged on failure), backup integrity (snapshots signed with the same KMS key family as Receipts), runbook + quarterly fire drills.
>
> **RPO / RTO targets committed:**
>
> | Component | RPO | RTO |
> | --- | --- | --- |
> | RDS Postgres | 5 min | 30 min (cross-AZ failover) |
> | Receipt-signing KMS | 0 | < 5 min |
> | Transparency log | 0 | < 15 min |
> | S3 (Receipts, sandbox) | 0 | < 5 min |
> | Full regional outage | 1 hour | 4 hours (warm-standby region) |

Written for the founding engineering team. Every design decision here has been stress-tested against the constraint of a 3-person engineering team shipping production in 12 weeks on a $1.5M pre-seed budget.

The document is opinionated by design. Engineering discipline is about what you *don't* build.

---

## 1. Engineering Principles

Seven principles that govern every decision:

1. **Boring technology wins.** PostgreSQL, Redis, Go, Temporal, AWS. No exotic data stores, no bleeding-edge frameworks, no language zoo. You can hire for these at 3am on a Saturday.

2. **One deployable in Year 1.** A modular monolith (`Optiqor-backend`) instead of nine microservices. Extract services only when a specific scaling or ownership boundary demands it — not before.

3. **Pick one language.** Go for the whole backend including the LLM orchestration layer. One dependency system, one test framework, one ops playbook. The Anthropic Go SDK is production-grade.

4. **Read heavy, write careful.** Unlimited read on customer clusters via ServiceAccount. Writes only through PRs the customer approves and merges. Optiqor never mutates a cluster directly, never merges a PR automatically (human click required — always, even in Enterprise).

5. **Determinism where we can, LLMs where we must.** Helm parsing, cost arithmetic, Prometheus query construction, policy evaluation, validation — all deterministic Go code. LLMs generate the fix diff and explain the reasoning. Never trust an LLM with arithmetic.

6. **Prometheus is the ground truth.** Every Confidence band and every rightsizing recommendation must be grounded in real customer Prometheus data. We never invent numbers. If data is missing, confidence is Low and we say why.

7. **Observability from commit #1.** The product gets instrumented before it gets a UI. If it's not measured, it's not shippable.

---

## 2. The Year-1 Stack (And Why)

### 2.1 The Shortlist

```
Application:      Go 1.23+ (modular monolith: Optiqor-backend)
Orchestration:    Temporal (workflows, retries, timeouts, async jobs)
Primary DB:       PostgreSQL 16 + TimescaleDB extension
Cache/Pub-Sub:    Redis 7
Object Storage:   S3 (logs, artifacts, receipts, CUR exports)
Messaging:        PostgreSQL LISTEN/NOTIFY (Year 1), NATS or Kafka (Year 2+)
Secrets:          AWS Secrets Manager + Vault
Deployment:       EKS (our own dogfood), managed via Helm + ArgoCD
CI/CD:            GitHub Actions → ECR → ArgoCD
Observability:    Prometheus, Grafana, Loki, OpenTelemetry, Sentry
Feature Flags:    OpenFeature (Unleash self-hosted)
LLM:              Anthropic (primary), OpenAI (fallback abstraction)
Client libs:      Anthropic Go SDK, client-go for Kubernetes
```

### 2.2 What's Explicitly NOT in the Year-1 Stack

These are good technologies. They're wrong for us right now.

| Rejected | Why | When to reconsider |
|----------|-----|---------------------|
| **Neo4j / graph DB** | PostgreSQL recursive CTEs handle 95% of our "graph" needs. Neo4j Community has no HA; Enterprise costs real money; operations expertise is scarce. | Year 3+ if we hit a query Postgres genuinely can't answer |
| **ClickHouse** | TimescaleDB on Postgres handles billions of rows. Adding ClickHouse = 2nd backup strategy, 2nd set of operational knowledge. | When Timescale hurts on aggregations (>500M rows or heavy cost-analytics workloads), probably late Year 2 |
| **Kafka** | Postgres LISTEN/NOTIFY + outbox pattern handles our Year-1 throughput (<1000 events/sec). | When throughput exceeds 5K events/sec or we need multi-consumer with different lag characteristics, Year 2+ |
| **Kubernetes for our app** | We'll run on EKS because we dogfood, but the complexity cost is real. | Staying here; it's non-negotiable for credibility |
| **Multiple languages** | Go for everything. Python in Year 2 only if ML training specifically demands it. | Month 18+ when we train classifiers |
| **Microservices** | Modular monolith until team hits 15+ or specific scaling walls | When ownership conflicts or deploy velocity actually hurts, probably Year 2 Q3+ |
| **Neptune / Dgraph / Snowflake / Databricks / BigQuery** | Zero Year-1 need | Not on the roadmap |

Every tech choice we reject saves ~3 weeks of integration, documentation, on-call burden, and backup strategy. We ship the flagship, not the platform.

### 2.3 Why Go, Not Python

The Agent Engine is the most LLM-heavy component, which historically meant Python. We're choosing Go anyway:

1. **One language, one deployment.** The alternative is Go ingestion + Python agent = two runtimes, two dependency managers, two Docker bases, two test frameworks. For a 3-person team, this compounds into weeks of lost time.
2. **Anthropic's Go SDK is production-grade.** Full streaming, tool use, retries, proper context cancellation. Feature parity with Python.
3. **Concurrency primitives we actually need.** Goroutines + channels map naturally to the "ingestion webhook → fan-out → Prometheus queries in parallel → LLM → validation → PR write" flow. `asyncio` works; Go's model is simpler.
4. **Binary size, startup time, memory footprint.** Matters for the in-cluster agent and for our own Kubernetes cost.
5. **Static typing catches a class of bugs that fail open in production.** Prompt output parsing, graph traversal, cost math — these are where dynamic typing bites.

Python only enters the stack in Year 2 if we train custom classifiers, and even then only in an isolated `ml-training` service invoked offline.

---

## 3. System Architecture

### 3.1 High-Level Shape

```
┌────────────────────────────────────────────────────────────────────┐
│                     Customer Infrastructure                        │
│                                                                    │
│   Customer EKS Cluster                    Customer GitHub Org      │
│   ┌──────────────────┐                   ┌──────────────────┐     │
│   │ Optiqor-agent  │ ServiceAccount    │  GitHub App      │     │
│   │ (Go, ~50m CPU)   │◄────read-only─────┤  (webhooks)      │     │
│   └────────┬─────────┘                   └────────┬─────────┘     │
└────────────┼──────────────────────────────────────┼───────────────┘
             │ mTLS egress                          │ HTTPS webhook
             ▼                                      ▼
┌────────────────────────────────────────────────────────────────────┐
│                     Optiqor SaaS (our EKS, us-east-1)            │
│                                                                    │
│   ┌────────────────────────────────────────────────────────────┐  │
│   │                      API Gateway (ALB + WAF)               │  │
│   └────────────────────────┬───────────────────────────────────┘  │
│                            │                                       │
│   ┌────────────────────────▼───────────────────────────────────┐  │
│   │            Optiqor-backend (Go, modular monolith)        │  │
│   │                                                            │  │
│   │   ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │  │
│   │   │ingestion │  │  parser  │  │  agent   │  │   cost   │  │  │
│   │   └──────────┘  └──────────┘  └──────────┘  └──────────┘  │  │
│   │   ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │  │
│   │   │ pr-writer│  │ receipts │  │ rollback │  │ api/web  │  │  │
│   │   └──────────┘  └──────────┘  └──────────┘  └──────────┘  │  │
│   │                                                            │  │
│   │       Temporal Workflows (long-running, idempotent)        │  │
│   └────────┬──────────────┬────────────────┬──────────────────┘  │
│            │              │                │                      │
│            ▼              ▼                ▼                      │
│   ┌──────────────┐ ┌──────────────┐ ┌──────────────┐              │
│   │  PostgreSQL  │ │    Redis     │ │      S3      │              │
│   │ + Timescale  │ │              │ │              │              │
│   │  (Multi-AZ)  │ │ (ElastiCache)│ │ (receipts,   │              │
│   │              │ │              │ │  artifacts)  │              │
│   └──────────────┘ └──────────────┘ └──────────────┘              │
│                                                                    │
│   ┌──────────────────────────────────────────────────────────┐    │
│   │              External: Anthropic, OpenAI, AWS CUR        │    │
│   └──────────────────────────────────────────────────────────┘    │
└────────────────────────────────────────────────────────────────────┘
```

### 3.2 Modular Monolith Package Layout

```
Optiqor-backend/
├── cmd/
│   ├── api/           # HTTP + webhook entry point
│   ├── worker/        # Temporal worker (same binary, different mode)
│   └── cli/           # Internal admin CLI
├── internal/
│   ├── ingestion/     # Cluster agent receiver, GitHub webhook handler
│   ├── parser/        # Helm, Kustomize, raw YAML parsers
│   ├── agent/         # LLM orchestration, prompt templates, validators
│   ├── cost/          # Pricing, CUR parsing, attribution
│   ├── prwriter/      # PR composition, Apply Fix signed tokens
│   ├── receipts/      # Ed25519 signing, verification workflows
│   ├── rollback/      # Signal monitors, rollback PR generation
│   ├── graph/         # Service-graph queries (pure Postgres, no Neo4j)
│   ├── confidence/    # Scoring rules, band computation
│   ├── sandbox/       # Public landing-page sandbox endpoint
│   ├── tenancy/       # Multi-tenant isolation, RBAC, API keys
│   └── platform/      # DB, cache, queue, metrics, logging, tracing
├── pkg/               # Public SDK bits (CLI uses these)
├── deploy/            # Helm charts for our own deployment
├── migrations/        # SQL migrations (sqlc + golang-migrate)
└── test/              # Integration tests against real Postgres/Redis
```

Single `go build ./cmd/api` produces the API binary. Single `go build ./cmd/worker` produces the Temporal worker. Same codebase, same deps, same container image, different entrypoint.

When the monolith gets too big (empirically, ~150K lines of Go), we carve out services on clear boundaries: first candidate is usually `sandbox` (it's public-facing and has different scaling characteristics), then `agent` (most expensive compute).

### 3.3 Deployment Topology

| Environment | Purpose | Infra |
|-------------|---------|-------|
| **dev** | Per-engineer ephemeral (Tilt + kind) | Local |
| **staging** | Production-identical, all tests run here | Small EKS cluster (2 nodes) |
| **prod** | Customer-facing | Multi-AZ EKS, 3-6 nodes baseline, autoscaling |
| **sandbox** | Isolated, stateless, public-facing for the landing-page sandbox | Separate namespace in prod cluster, aggressive resource limits, heavy rate limiting |

Production is deployed to `us-east-1` with Multi-AZ. Year 2 adds `eu-west-1` for EU data residency compliance (GDPR / CSRD).

### 3.4 What Runs In the Customer Cluster

A single opinionated Go binary, deployed via Helm chart:

```yaml
# values.yaml (defaults)
Optiqor:
  token: "one-time-bootstrap-token"  # exchanged for persistent token on first contact
  mode: active                        # or "skeptic" for passive-observer deployment
  resources:
    requests: {cpu: "50m", memory: "128Mi"}
    limits:   {cpu: "500m", memory: "512Mi"}
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    readOnlyRootFilesystem: true
    allowPrivilegeEscalation: false
    capabilities: { drop: [ALL] }
  egress:
    endpoint: "https://ingest.optiqor.dev"
    proxy: ""  # customer may route through corporate egress proxy
```

**What the agent does:**
- Watches Kubernetes API (client-go informers) for Deployment/StatefulSet/HPA/Pod events
- Scrapes Prometheus at defined intervals for cost-relevant metrics
- Ships compressed, batched data to our SaaS over mTLS
- Responds to on-demand Prometheus query requests (for PR analysis)

**What the agent does NOT do:**
- No write access to the cluster. RBAC is `ClusterRole` with only `get`, `list`, `watch` verbs.
- No local state beyond ephemeral buffers
- No hostPath, hostNetwork, or privileged containers
- No direct internet egress — all traffic flows to `ingest.optiqor.dev` or a customer-controlled egress proxy

**Outbound-only network — the enterprise security promise.** The agent **never accepts inbound connections**, ever. No exposed ports, no NodePort/LoadBalancer service, no firewall rules to open, no peering, no VPN, no port-forward, no inbound webhook receiver. Every byte of agent ↔ SaaS traffic is initiated by the agent over mTLS to a single Optiqor endpoint. The customer's firewall stays closed; their security team approves the install in a one-page review instead of a one-quarter network review. This is how Datadog, New Relic, and Grafana Agent earned enterprise adoption, and it's how we will. Combined with the read-only RBAC above, the worst-case compromise of the agent reads workload metadata; it cannot modify production. **State this on the website, in the SOC 2 prep deck, and in the procurement Q&A response template** — it converts security review from a blocker into a checkbox.

**Agent is open-source (Apache 2.0)** at `github.com/optiqor/agent`. Customers audit source, verify signed binaries (Sigstore), review SBOMs. This is the only repo we make public in Year 1.

**No-Agent Mode** (enterprise security unlock): customers who cannot install the agent provide kubeconfig + Prometheus remote-read endpoint. Optiqor runs a polling worker on our side. Same functionality, ~60% data richness, slightly higher latency.

**Agent footprint SLO:** the agent's steady-state resource use is non-negotiable — **<100m CPU, <128MB memory on a typical 200-workload cluster**. A heavy agent becomes a permanent operational tax customers notice ("the irony of your K8s cost tool itself being a meaningful cost is the kind of detail enterprise buyers flag in G2 reviews"). Bandwidth budget: **<100MB/day** of compressed metric data uploaded to SaaS. These targets are tracked against the agent's own Prometheus metrics and validated post-launch; missing them is a P1 regression.

**Agent update model — customer's GitOps reconciles, not us.** The agent is itself a Helm chart. Optiqor publishes new versions to the agent chart repo (`charts.optiqor.dev/agent`); the customer's ArgoCD or Flux installation reconciles the change on their schedule. We **never** auto-update an agent running in a customer cluster. The architectural reasons:
- Security teams want to control what runs in production.
- Optiqor sells GitOps to customers as the change-management primitive; auto-updating our own infrastructure outside that workflow would be hypocritical and operationally risky.
- The customer can pin to an older version indefinitely; we maintain support windows publicly.
- New customer onboarding installs via `helm install optiqor/agent` (or equivalent ArgoCD `Application`); the install is the same shape as every other Helm chart the customer manages.

### 3.5 What Optiqor Is — And Isn't (Coexistence with K8s Primitives)

**Optiqor is the intelligence layer above Kubernetes autoscaling primitives. We coexist with VPA, HPA, and Karpenter — we do not replace them.** See ADR-0012 (`docs/adr/0012-coexist-with-primitives.md`) for the architectural commitment.

| Primitive | What it does | How Optiqor relates |
|---|---|---|
| **VPA recommender** | Histogram-based per-pod sizing | Read its recommendations as one signal (when in Off mode); produce the canonical recommendation when VPA is absent or off; disable/warn when VPA is in Auto mode for a workload Optiqor would also act on. Never uninstall. |
| **HPA** | Replica scaling against a metric | Read the HPA spec to know it exists; size accordingly (HPA-aware sizing differs materially from static-replica sizing — this is the "bi-dimensional" insight). Optionally recommend HPA parameter changes (target utilization, custom metrics). Never replace. |
| **Karpenter** | Node-layer autoscaling, consolidation, Spot interruption | Read NodePool config to ground recommendations; attribute pod cost across the node lifetimes Karpenter manages (consolidation moves pods — this affects 30-day cost calculations); optionally recommend NodePool changes via a separate PR shape (Karpenter CRD, not workload YAML). Never replace. |
| **VPA in Auto mode** | Actively resizing pods in production (rare) | Optiqor disables itself for that workload, or warns the user explicitly. Two systems sizing the same workload race each other. |

**The one place Optiqor's own logic is genuinely original work:** the **auto-rollback guard**. When a merged Optiqor fix starts misbehaving, none of VPA/HPA/Karpenter knows it was Optiqor's fault — they react to "the pod is failing" in their own ways (HPA scales replicas; VPA in Auto mode increases requests; Karpenter provisions nodes). Optiqor's statistical pre/post-merge anomaly detection + automated rollback PR is the only component that closes that loop. This is the moat that justifies "we don't replace, we add intelligence."

The pitch sentence that lands: *"Optiqor works alongside your existing VPA, HPA, and Karpenter. We don't replace them — we make them smarter by giving them better-calibrated input. Your autoscaling primitives stay where they are; Optiqor decides what values they should run with."*

---

## 4. Data Model

### 4.1 Why PostgreSQL Alone

Every "graph" the doc previously wanted Neo4j for is one of the following:

| Query pattern | Frequency | Postgres solution |
|---------------|-----------|-------------------|
| "What workloads does `checkout-api` depend on?" | High | `service_edges` table + recursive CTE, depth-bounded |
| "What PRs modified this Helm chart in the last 90d?" | High | Indexed join: `pull_requests` ↔ `iac_files` |
| "What's the blast radius of this Deployment?" | Medium | Recursive CTE with depth limit = 2, cached in Redis |
| "Which PR caused this cost spike?" | Medium | Time-bounded join on `cost_events` + `pr_merges` |
| "What's our cross-customer 'similar change' match?" | Low (Year 2+) | `pgvector` embedding similarity |

All of these run in milliseconds on well-indexed Postgres. None need a dedicated graph database.

**Service graph is materialized, not queried live.** We compute the graph once every 15 min and cache it in Redis with a 1-hour TTL. Blast-radius queries hit Redis in <10ms.

### 4.2 Schema (Core Tables)

> **Canonical schema lives in [optiqor/migrations/0001_baseline.sql](../../migrations/0001_baseline.sql).** The SQL below pre-dates the baseline and remains as a *design sketch* — table names and column shapes drift from what shipped (e.g. baseline uses `tenants` not `customers`, has a `workspaces` layer the sketch omits, models receipts with a three-tier `tier` enum). When the sketch disagrees with the baseline, the baseline wins. The migration plan in §4.2.1 evolves the baseline forward.

All tables have `tenant_id` for row-level security. RLS policies enforce that no query returns another customer's data, even on bugs.

```sql
-- Customers and tenancy
CREATE TABLE customers (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    github_org TEXT NOT NULL UNIQUE,
    tier TEXT NOT NULL CHECK (tier IN ('free','team','team_plus','enterprise')),
    k8s_spend_band TEXT,  -- 'small','medium','large','enterprise'
    mode TEXT NOT NULL DEFAULT 'active' CHECK (mode IN ('skeptic','active')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE clusters (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES customers(id),
    name TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT 'eks',
    region TEXT NOT NULL,
    agent_version TEXT,
    last_seen_at TIMESTAMPTZ,
    deployment_mode TEXT CHECK (deployment_mode IN ('agent','no_agent')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Kubernetes inventory (polled every 15 min, current state)
CREATE TABLE workloads (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES customers(id),
    cluster_id UUID NOT NULL REFERENCES clusters(id),
    namespace TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('Deployment','StatefulSet','DaemonSet','Job','CronJob','ReplicaSet')),
    name TEXT NOT NULL,
    owner_kind TEXT,  -- e.g., 'Prometheus' if owned by Prometheus Operator
    owner_name TEXT,
    source_iac_repo TEXT,
    source_iac_path TEXT,
    spec JSONB NOT NULL,  -- full current PodSpec
    replicas INT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(cluster_id, namespace, kind, name)
);
CREATE INDEX ON workloads USING GIN (spec);
CREATE INDEX ON workloads (tenant_id, cluster_id);

-- Service dependency graph (materialized every 15 min)
CREATE TABLE service_edges (
    tenant_id UUID NOT NULL REFERENCES customers(id),
    source_workload_id UUID NOT NULL REFERENCES workloads(id),
    target_workload_id UUID NOT NULL REFERENCES workloads(id),
    source TEXT NOT NULL CHECK (source IN ('istio','linkerd','labels','prometheus','manual')),
    weight REAL DEFAULT 1.0,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source_workload_id, target_workload_id)
);

-- IaC file tracking
CREATE TABLE iac_files (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES customers(id),
    repo TEXT NOT NULL,
    path TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('helm_values','helm_chart','kustomize_overlay','kustomize_base','raw_manifest','argocd_application')),
    last_commit_sha TEXT NOT NULL,
    last_modified_at TIMESTAMPTZ,
    UNIQUE(repo, path)
);

-- Pull requests (ours and customer's)
CREATE TABLE pull_requests (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES customers(id),
    github_id BIGINT NOT NULL,
    repo TEXT NOT NULL,
    number INT NOT NULL,
    author TEXT,
    Optiqor_role TEXT CHECK (Optiqor_role IN ('observer','apply_fix','rollback','savings_proposal')),
    status TEXT NOT NULL,
    confidence_band TEXT CHECK (confidence_band IN ('low','medium','high')),
    confidence_raw REAL,
    predicted_cost_delta_usd_month REAL,
    merged_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(repo, number)
);
CREATE INDEX ON pull_requests (tenant_id, merged_at DESC) WHERE merged_at IS NOT NULL;

-- Findings from agent engine
CREATE TABLE findings (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES customers(id),
    workload_id UUID NOT NULL REFERENCES workloads(id),
    type TEXT NOT NULL,  -- 'overprovisioned_cpu', etc.
    severity TEXT,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    pr_id UUID REFERENCES pull_requests(id),
    status TEXT NOT NULL DEFAULT 'open',
    metadata JSONB
);

-- Receipts
CREATE TABLE receipts (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES customers(id),
    pr_id UUID NOT NULL REFERENCES pull_requests(id),
    predicted_delta_usd_month REAL NOT NULL,
    actual_delta_usd_month REAL,
    methodology_version TEXT NOT NULL,
    cur_snapshot_url TEXT,  -- S3 URL to the raw CUR rows used
    signature_ed25519 BYTEA,
    signing_key_id TEXT,
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Time-series (TimescaleDB hypertables)
CREATE TABLE workload_metrics (
    tenant_id UUID NOT NULL,
    workload_id UUID NOT NULL,
    ts TIMESTAMPTZ NOT NULL,
    cpu_usage_cores REAL,
    cpu_requests_cores REAL,
    mem_usage_bytes BIGINT,
    mem_requests_bytes BIGINT,
    replicas INT,
    PRIMARY KEY (workload_id, ts)
);
SELECT create_hypertable('workload_metrics', 'ts', chunk_time_interval => INTERVAL '1 day');
SELECT add_retention_policy('workload_metrics', INTERVAL '90 days');

CREATE TABLE cost_attributions (
    tenant_id UUID NOT NULL,
    workload_id UUID NOT NULL,
    day DATE NOT NULL,
    compute_cost_usd NUMERIC(10,4) NOT NULL,
    network_cost_usd NUMERIC(10,4) DEFAULT 0,
    storage_cost_usd NUMERIC(10,4) DEFAULT 0,
    attribution_method TEXT NOT NULL,  -- 'requests_weighted','usage_weighted','hybrid_v1'
    PRIMARY KEY (workload_id, day)
);
SELECT create_hypertable('cost_attributions', 'day', chunk_time_interval => INTERVAL '7 days');
```

### 4.2.1 Planned migrations beyond the baseline

The committed baseline ([optiqor/migrations/0001_baseline.sql](../../migrations/0001_baseline.sql)) covers the 5-level hierarchy (`tenants → workspaces → clusters → namespaces → workloads`) plus `recommendations`, `recommendation_dismissals`, `apply_fixes`, `receipts` (with `tier ∈ {cloud, capacity, hybrid}`), `llm_calls`, `audit_log`, RLS via `current_setting('app.tenant_id', true)`, and the `optiqor_app` / `optiqor_migrator BYPASSRLS` role split. **Nine** follow-up migrations are queued, each behind its triggering feature. Order is deterministic; detail tracked in [optiqor/todo.md](../../todo.md).

| Migration | Lands with | Locked design choices |
| --- | --- | --- |
| `0002_workload_observed_state.sql` | Phase 1 follow-up, before Phase 5 agent | Additive cols on `workloads`: `container_image TEXT` (**non-negotiable from row one** — pattern-library moat) · `replicas INT` · `has_hpa BOOL` · `current_cpu_request_millicores`, `current_memory_request_bytes`, `current_cpu_limit_millicores`, `current_memory_limit_bytes` · `last_observed_at TIMESTAMPTZ`. Agent reconciler is sole writer. Index on `container_image` for cross-tenant pattern queries (used under `is_superuser_context()`, see §4.2.2). |
| `0003_tenancy_primitives.sql` | Phase 1 follow-up, paired with `0002` | Pure additive — does not change existing table structure, only refactors RLS policies to read through new helpers. UUID v7 generator · `current_tenant_id()` helper · `is_superuser_context()` per-query bypass · `set_updated_at()` trigger. **Variable name stays `app.tenant_id`** to match `internal/platform/db` bind helper. See §4.2.2. |
| `0004_shared_analyses.sql` | Phase 2 sandbox hardening | `hash TEXT UNIQUE`, `payload_sha256`, `findings_json JSONB`, `source CHECK ('cli','sandbox')`, `view_count`, `created_at`, `expires_at`. Public-by-design, **no RLS**. In-row payload until a single share exceeds ~256 KiB, then promote payload to S3 (pointer stays in Postgres). |
| `0005_auth.sql` | Phase 5 dashboard go-live | `users` (global, **no RLS** — a human can belong to many tenants; the auth subsystem is sole reader/writer) · `memberships` (RLS-scoped, role CHECK `owner/admin/member/viewer`) · `api_tokens` (RLS-scoped, `token_hash BYTEA`, `scopes TEXT[]`, `last_used_at`, `expires_at`, `revoked_at`). Token validation runs on the `optiqor_migrator BYPASSRLS` connection until tenant is resolved; then `set_config('app.tenant_id', ...)` switches to the regular pool. |
| `0006_metric_samples.sql` | Phase 5 agent watch loop | `create_hypertable('metric_samples','time', chunk_time_interval => '1 day')` · compress `segmentby='workload_id', orderby='time DESC'`, `add_compression_policy(INTERVAL '7 days')` · retain `add_retention_policy(INTERVAL '35 days')` (30-day window + 5-day buffer) · continuous aggregate `metric_samples_hourly` materialising `avg / max / approx_percentile(0.95) / approx_percentile(0.99)` via TimescaleDB-toolkit `percentile_agg`; refresh `start_offset=35d / end_offset=1h / schedule=30min`. **Sizing engine reads the aggregate, not raw.** RLS via `tenant_id`. No FKs (hypertable convention; referential integrity is app-enforced and verified by a nightly cross-table sanity check). |
| `0007_billing_line_items.sql` | Phase 5 → 6, when first CUR ingest lands in Postgres | Hypertable, 1-day chunks. **Two enum columns mandatory from row one — impossible to retrofit cleanly:** `source TEXT CHECK ('aws_cur','azure_cost_mgmt','hetzner_invoice','capacity_deferred')` (which bill, drives 3-tier Receipt routing) and `pricing_mode TEXT CHECK ('spot','on_demand','savings_plan','reserved','other')` (rate within the bill). Compress `segmentby='cluster_id'` after 7d; retain 365d (financial data; Receipts cite it). |
| `0008_stripe_mirror.sql` | Phase 6 Stripe billing | Additive col `tenants.stripe_customer_id TEXT UNIQUE` · `subscriptions` (one row per Stripe subscription — a tenant can have many over time) · `usage_records` · `invoices`. Reconciliation via Stripe webhook → `internal/api/webhooks`. |
| `0009_vcs_installations.sql` | Phase 4 GitHub App go-live (applied alongside 0005–0008 in one deployment) | One row per GitHub/GitLab App installation per tenant — multi-VCS and multi-org enterprise both demand many installations per tenant: `tenant_id`, `provider TEXT CHECK ('github','gitlab')`, `installation_id BIGINT`, `account_login TEXT`, `access_token_ciphertext BYTEA` (KMS-encrypted; tokens expire hourly and are refreshed in-place), `token_expires_at TIMESTAMPTZ`, `installed_at`, `revoked_at`, `status TEXT CHECK ('active','suspended','revoked')`, `UNIQUE (provider, installation_id)`. RLS-scoped. **Without this table** the webhook handler can't refresh the GitHub App access token after the first hour and can't disambiguate multi-org enterprise tenants. |
| `0010_audit_log_partitioning.sql` | Phase 8 SOC 2 Type 1 prep | Convert `audit_log` from a normal table to a TimescaleDB hypertable on `occurred_at` (1-month chunks), `add_compression_policy(INTERVAL '30 days')`, `add_retention_policy(INTERVAL '7 years')`. 7-year retention with billions of rows is a non-starter on a normal heap — `ALTER TABLE` slows to minutes by Y3 without partitioning. Hypertable conversion is online and lossless. Append-only DML grants stay (`REVOKE UPDATE, DELETE`). |

**Design calls deliberately deferred** (decision when the trigger fires, not before):

- **Denormalised current state on `workloads` vs derive-from-`metric_samples`.** Default is denormalise — one writer (agent reconciler) keeps consistency, dashboard reads stay fast. Revisit when the first dashboard latency budget bites.
- **`onboarding_progress` table vs `tenants.onboarding_state` JSONB.** JSONB is fine while nudge cadence is hard-coded. Split into a dedicated table when nudges become customer-tunable, per-stage SLA reporting lands, or activation-funnel charting wants per-step time-in-state.
- **`shared_analyses` body in-row vs S3 pointer.** Start in-row (one small table is operationally trivial). Promote payload to S3 only if a single share exceeds ~256 KiB.
- **Long-horizon downsample of `metric_samples_hourly`.** Continuous aggregate has no retention policy at launch. Daily/weekly downsamples come only if storage cost demands them.
- **Promote `workloads.workload_class_group_id UUID` into a real `workload_classes` table.** Today the group-id is a free-floating UUID indexed for cross-cluster fleet queries — enough for Y1's "apply this fix to all 5 instances" story. A real table earns its weight only when a class needs to carry metadata (description, customer-tunable snooze rules, class-level blast-radius overrides) or when the dashboard ships a per-class detail page. Promote in Phase 7 alongside the workload classifier rollout if fleet-wide Apply Fix has a UI; defer otherwise.

### 4.2.2 Tenancy primitives (locked in `0003_tenancy_primitives.sql`)

Four additive helpers that pay back on every future migration. `0003` does not alter the structure of any baseline table — it adds functions, then rewrites existing RLS policies to read through them. Semantics unchanged.

```sql
-- UUID v7: time-ordered, better B-tree locality on append-heavy hot tables
-- (metric_samples_hourly, audit_log, recommendations). Existing v4 UUIDs on
-- baseline tables stay untouched; DEFAULT uuid_generate_v7() applies to future
-- inserts only. Drop this function when PG18 ships native uuidv7().
CREATE OR REPLACE FUNCTION uuid_generate_v7() RETURNS uuid AS $$
DECLARE unix_ts_ms bytea; uuid_bytes bytea;
BEGIN
    unix_ts_ms := substring(int8send((extract(epoch FROM clock_timestamp()) * 1000)::bigint) FROM 3);
    uuid_bytes := unix_ts_ms || gen_random_bytes(10);
    uuid_bytes := set_byte(uuid_bytes, 6, (b'0111' || get_byte(uuid_bytes, 6)::bit(4))::bit(8)::int);
    uuid_bytes := set_byte(uuid_bytes, 8, (b'10'   || get_byte(uuid_bytes, 8)::bit(6))::bit(8)::int);
    RETURN encode(uuid_bytes, 'hex')::uuid;
END $$ LANGUAGE plpgsql VOLATILE;

-- Tenant resolver. Variable name MUST stay app.tenant_id (matches the existing
-- internal/platform/db bind helper).
CREATE OR REPLACE FUNCTION current_tenant_id() RETURNS uuid AS $$
    SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
$$ LANGUAGE sql STABLE;

-- Per-transaction RLS bypass for legitimate cross-tenant background jobs
-- (cross-customer pattern-library aggregation, nightly metrics rollup,
-- transparency-log writer). Off by default. Every flip to 'on' is logged in
-- audit_log with the calling workflow + actor.
CREATE OR REPLACE FUNCTION is_superuser_context() RETURNS boolean AS $$
    SELECT COALESCE(current_setting('app.bypass_rls', true), 'off') = 'on';
$$ LANGUAGE sql STABLE;

-- updated_at trigger. Attach to every table that has an updated_at column.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN NEW.updated_at := now(); RETURN NEW; END $$ LANGUAGE plpgsql;

-- Refactor every baseline RLS policy to read through the helpers. Semantics
-- unchanged; the policy body becomes shorter and the bypass flag becomes
-- usable. Run for: workspaces, clusters, namespaces, workloads, recommendations,
-- recommendation_dismissals, apply_fixes, receipts, llm_calls, audit_log.
ALTER POLICY tenant_isolation ON workloads
    USING (tenant_id = current_tenant_id() OR is_superuser_context());
-- (repeat for the other nine tenant-scoped tables)
```

**Why these specific picks:**

- **UUID v7 not v4 for hot tables.** `metric_samples_hourly`, `audit_log`, `llm_calls` will see millions of inserts per tenant per day. Time-ordered IDs keep the B-tree's right edge hot and avoid random-insert page splits. Existing v4 IDs on baseline tables stay; new tables and append-heavy hot tables get v7.
- **Per-transaction bypass flag, not blanket BYPASSRLS.** The pattern-library aggregation job legitimately reads across tenants. Forcing it onto the `optiqor_migrator` connection (BYPASSRLS at role level) is dangerous if that connection ever leaks. The flag scopes the bypass to a single transaction, and every flip writes an `audit_log` row so cross-tenant reads are accountable.
- **No FKs on hypertables.** TimescaleDB chunk routing interacts poorly with FK enforcement at scale. Industry-standard pattern; we compensate with a nightly cross-table sanity check (count of `metric_samples.workload_id` not in `workloads` must be 0; alerts oncall otherwise).
- **`users` global, no RLS.** A human can belong to many tenants. The `users` table is only ever read/written by the auth subsystem; everything else joins through `memberships` which is RLS-scoped. Enforcement-by-discipline boundary, documented in `internal/platform/db`.

### 4.3 Multi-Tenancy: Row-Level Security Is Non-Negotiable

Every tenant-scoped table has `tenant_id` + `ENABLE ROW LEVEL SECURITY` + a `tenant_isolation` policy that reads through the helpers from §4.2.2. RLS makes cross-tenant leaks impossible at the database layer, even on app-code bugs:

```sql
-- Pattern, applied to every tenant-scoped table by 0001_baseline.sql and
-- rewritten to call current_tenant_id() / is_superuser_context() by 0003.
ALTER TABLE workloads ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workloads
    USING (tenant_id = current_tenant_id() OR is_superuser_context());
```

Before each request, [internal/platform/db](../../internal/platform/db/) opens a transaction and calls `set_config('app.tenant_id', <uuid>, true)`. The variable is `LOCAL` to the transaction, so a bug that forgets to set it returns *zero rows*, never another customer's data. The `is_superuser_context()` branch is opt-in per transaction (`SET LOCAL app.bypass_rls = 'on'`) and writes an `audit_log` row on every flip — reserved for cross-tenant background jobs like the Helm Chart Efficiency Leaderboard aggregation, six-metric health rollups, and cross-customer pattern-library training.

**Two connection roles, not one:**

- `optiqor_app` (NOLOGIN, RLS-subject) — every API request and every Temporal worker activity. Cannot bypass RLS by design.
- `optiqor_migrator` (NOLOGIN, BYPASSRLS) — schema migrations only. Never used for runtime queries.

For high-tier Enterprise customers we can go further: per-customer Postgres schema or dedicated logical database. The shared-table + RLS + bypass-flag model is the Year-1 default; schema isolation is a Month-15+ enterprise unlock that doesn't change the rest of the code.

### 4.4 Consistency and Caching

- **Postgres is the source of truth.** Every write goes here first, synchronously.
- **Redis is a cache, never authoritative.** Keys include the tenant ID; TTLs are short (15 min typical, 1 hour max). If Redis dies, latency degrades, correctness doesn't.
- **S3 for large artifacts.** Receipt PDFs, CUR snapshots, LLM audit logs. Lifecycle policies move to Glacier after 90 days.
- **Eventual consistency boundaries are explicit.** Graph materialization is eventually consistent (≤15 min lag). Prometheus metrics are eventually consistent. Billing and receipts are strongly consistent.

---

## 5. Ingestion

### 5.1 In-Cluster Agent

Go binary. Single container. The surface area is deliberately tiny.

```go
// Conceptual — actual implementation uses client-go informers, not polling
type Agent struct {
    k8sClient    kubernetes.Interface
    promClient   prometheus.API
    egress       *EgressClient   // mTLS to ingest.optiqor.dev
    mode         Mode            // Active, Skeptic, Discovery
}

// Watches K8s API, ships events
func (a *Agent) WatchCluster(ctx context.Context) error {
    informerFactory := informers.NewSharedInformerFactory(a.k8sClient, 30*time.Second)
    deploymentInformer := informerFactory.Apps().V1().Deployments().Informer()
    deploymentInformer.AddEventHandler(&DeploymentHandler{egress: a.egress})
    // ... similar for StatefulSets, HPAs, Pods, Events
    informerFactory.Start(ctx.Done())
    informerFactory.WaitForCacheSync(ctx.Done())
    <-ctx.Done()
    return nil
}
```

**Memory ceiling:** `GOMEMLIMIT=384Mi`. The agent OOMs itself before taking cluster memory.

**Network:** all outbound traffic is to `ingest.optiqor.dev:443` over mTLS. Certificate is pinned. Customer proxy supported via `HTTPS_PROXY` env var.

**Resilience:** if `ingest.optiqor.dev` is down, the agent buffers up to 4 hours of events in an in-memory ring buffer, then drops oldest. It does not write to disk. It does not retry indefinitely.

### 5.2 GitHub App

Standard GitHub App with minimal permissions:

```
Read: contents, metadata, pull_requests
Write: pull_requests, issues (for PR comments)
Events: pull_request, push (default branches only), installation
```

**Never** read/write on default branches directly. **Never** request `admin`, `actions`, or `deployments` permissions.

**Two-surface PR shape — receipt-signed vs PR-rendered.** Every Optiqor PR has two surfaces with different determinism requirements:

- **Receipt-signed surface** — the numeric values, methodology metadata, commit content hash, recommendation ID. This is what the eventual Receipt cryptographically attests to. Must be deterministic and math-only.
- **PR-rendered surface** — title, description prose, inline review comments, confidence-band explanation. This is what the customer reads when deciding whether to merge. LLM-helpful prose with deterministic *structured fields embedded*. Passes through the ADR-0007 output validator before reaching GitHub.

Conflating these two leads to either under-validation (LLM hallucinations reach the customer) or over-restriction (the PR reads like a robot, hurting Suggest-mode adoption). The correct split:

| Surface | Determinism | Source | Notes |
|---|---|---|---|
| Branch name (`optiqor/<rec-id>`) | Deterministic | UUID v7 from `recommendations` row | Operational hygiene + chronological sort |
| YAML diff content | Deterministic | Methodology library output | What receipts effectively attest to |
| Recommended numeric values | Deterministic | Methodology library output | Receipt-signed |
| Cost projection ($ figure) | Deterministic | Methodology library output | Receipt-signed |
| Confidence band token (HIGH / MED / LOW) | Deterministic | Methodology library output | Gates Apply Fix dispatch + receipt-signed |
| Commit body — methodology metadata block | Deterministic template | Templated from the row | Audit chain |
| Commit subject line | **LLM-helpful** | LLM, validated | `optiqor(<workload>): <subject>` prefix is templated; the `<subject>` is LLM prose, capped at 72 chars |
| PR title | **LLM-helpful** | LLM, validated | Customers read this in the GitHub PR list |
| PR description prose (the "What changed" / "Why" narrative) | **LLM-helpful** | LLM, validated | The cost-impact table, methodology link, validation checks are deterministic structured fields *embedded* in the prose |
| Inline PR comments (per-hunk annotations) | **LLM-helpful** | LLM, validated | Pointers to specific YAML changes, e.g. "this 250m → 180m is based on P95 over the last 14 days" |
| Confidence band display | Deterministic value + **LLM-helpful prose summary** | Methodology emits the band; LLM explains why | e.g. *"HIGH — 14 days of data, P95 utilization stable at ~72% of request"* |

**Branch naming:**

```
optiqor/<recommendation-id>
```

`<recommendation-id>` is the UUID v7 from the `recommendations` table. UUID v7's time-ordered prefix means branch names sort chronologically in `git branch --list optiqor/*` — useful for operators sweeping abandoned branches. Never embed customer-sensitive strings in the branch name.

**Commit message shape:**

```
optiqor(<workload>): <LLM-generated subject, ≤72 chars>

<LLM-generated 2-3 sentence narrative explaining the change>

----- methodology metadata (deterministic, audit chain) -----
Recommendation:    <recommendation-id>
Workload:          <namespace>/<kind>/<name>
Methodology:       hybrid_v1
Confidence:        <high|med|low>
Projected savings: $<usd>/month
```

The `optiqor(<workload>):` prefix and the methodology-metadata block are templated and deterministic. The subject text after the prefix, and the narrative paragraph, are LLM-generated and pass through output validation (length bounds, no numeric values that contradict the methodology block, no profanity). **The LLM is allowed to write the commit subject and narrative** because the receipt records the commit's SHA after the fact — it doesn't pre-determine the commit text. Reproducibility of the commit text is a nice-to-have for sweep tooling, not a load-bearing audit property.

**PR description template:**

```markdown
## <LLM-generated title, also used as the PR title>

<LLM-generated 2-3 sentence narrative — "What we found, what we propose,
what it costs.">

## Cost impact <!-- deterministic structured field, embedded in prose -->

| Before | After | Monthly savings |
|---|---|---|
| <current values from methodology> | <recommended values from methodology> | <$X from methodology> |

## Why <!-- LLM-helpful prose, validated -->

<LLM-generated explanation of the finding, written for the customer
reading on GitHub. Pulls finding detail from the detector library; the
LLM rewrites it for clarity. Cites the same numbers as the table above —
output validator confirms.>

## Confidence <!-- deterministic band + LLM-helpful summary -->

**<HIGH | MED | LOW>** — <LLM-generated 1-2 sentence summary referencing
the observed data window and signal stability. Numeric claims (days of
data, P95 percentage) come from methodology; LLM writes the framing.>

## How we know <!-- deterministic structured field -->

- Methodology: hybrid_v1 (`optiqor.dev/methodology/hybrid-v1`)
- Validation: helm template ✓ · kubeconform ✓ · dry-run-server ✓ (per ADR-0010)
- Observed window: <N> days

## Receipt <!-- deterministic boilerplate -->

A signed Receipt verifying actual savings against your cloud bill will be
posted 30 days after merge. Track at `optiqor.dev/receipts/<recommendation-id>`.

---
*PR opened by Optiqor. Comment `/optiqor dismiss` to dismiss (per ADR-0009 lifecycle).*
```

**Inline review comments.** Where the diff spans multiple files or non-obvious hunks, the PR Writer also leaves inline GitHub review comments on specific lines — "this line: we lowered `requests.cpu` to 180m because observed P95 over the last 14 days was 145m." These are **LLM-helpful prose** with the deterministic value (180m, 145m) interpolated from methodology output. The output validator checks that interpolated numbers match the diff and methodology before posting.

**Determinism rules for the PR shape (corrected):**

1. **The audit chain is deterministic, the customer-facing prose is not.** Branch name, YAML diff content, methodology metadata block, cost-impact table values, confidence band token, the "How we know" + "Receipt" sections — all reproducible from `recommendations` + `findings`. PR title, commit subject, narrative paragraphs, "Why" prose, confidence band explanation — LLM-generated and validated.
2. **No timestamps in any LLM-generated text.** Use the recommendation's `created_at` only inside deterministic structured fields; never `time.Now()` at render time. Per CLAUDE.md, the `Clock` interface is injected.
3. **No randomness in branch names or structured fields.** UUID v7 from the row; no salts.
4. **Output validation is mandatory before posting to GitHub.** Per ADR-0007: prose passes the length-bounds + profanity + numeric-consistency check. The validator strips any LLM-inserted numeric claim that doesn't match the methodology output and substitutes the methodology value.
5. **Tests pin the deterministic surfaces.** Golden fixtures in `internal/prwriter/testdata/` assert byte-identical output for: branch name, methodology metadata block, cost-impact table, "How we know" section, "Receipt" section. **LLM-generated surfaces are exempt from golden tests** — they're covered by validator tests instead (does the output respect length bounds, contain no contradictory numbers, pass profanity check).
6. **What the eventual Receipt actually signs over.** `(commit_sha_at_merge, predicted_savings, methodology_version, recommendation_id, timestamp)`. The commit SHA is recorded after the merge, not pre-determined from text reproducibility. The audit chain doesn't depend on PR-text byte-stability — it depends on the diff content (YAML changes) being reproducible from methodology + the commit SHA being faithfully recorded.

Token handling: customer-level installation tokens are short-lived (1 hour). We refresh proactively. The refresh token is stored encrypted (Postgres column encryption with AWS KMS).

**Envelope encryption pattern (DEK / KEK).** Customer secrets in the database — GitHub App installation tokens, customer AWS access keys when supplied for CUR access, Anthropic BYO keys for enterprise tier — use envelope encryption rather than direct KMS-per-row encryption:

```
KEK (Key Encryption Key)    — lives in AWS KMS, never leaves
   ↓ wraps
DEK (Data Encryption Key)   — randomly generated per row, ephemeral
   ↓ encrypts
Token / secret              — at rest in Postgres, paired with the wrapped DEK
```

For every row that stores a customer secret:
1. Generate a fresh 256-bit DEK with `crypto/rand`.
2. Encrypt the secret with the DEK (AES-256-GCM).
3. Call KMS `Encrypt(KEK, DEK)` to wrap the DEK; KMS never sees the secret, only the DEK.
4. Store `{ciphertext, wrapped_dek, kek_arn, kek_version}` in the row.

At read time, the reverse: fetch the wrapped DEK, call KMS `Decrypt(wrapped_dek)` to unwrap (only this single decrypt happens per read), use the plaintext DEK to decrypt the ciphertext. The DEK exists in process memory only for the duration of one decrypt + use; the KEK never leaves KMS.

Why envelope rather than direct KMS encryption:
- **KMS rate limits.** Direct KMS encrypt/decrypt is rate-limited per region (~10K req/s). Envelope encryption batches: one KMS call generates a DEK that encrypts many secrets, or wraps one DEK per row but only unwraps on access.
- **Key rotation independence.** Rotating the KEK doesn't require re-encrypting every ciphertext, only re-wrapping the DEKs. Rotating a DEK doesn't require KMS calls at all — generate a new DEK locally, re-encrypt the affected secrets, wrap with the same KEK.
- **Smaller blast radius.** A SQL injection or backup leak exposes wrapped DEKs and ciphertexts. Without KMS access (IAM scope), the attacker cannot decrypt anything. A compromised KMS API session can unwrap DEKs only for rows the session reads — not the whole database.
- **Audit granularity.** Every KMS unwrap produces a CloudTrail entry. Whoever accessed a customer secret leaves a trail with row-level resolution.

The envelope pattern is implemented in `internal/platform/db/crypto/`. Direct `kms.Encrypt(secret)` calls outside the wrapper are a P0 bug.

### 5.3 Parsing

**Helm.** We embed `helm/v3` as a Go library, not the CLI. We render with customer `values.yaml`. We track the delta between templated Kubernetes objects and their source. We do NOT execute arbitrary Helm hooks; only templating runs in our sandbox.

**Kustomize.** We embed `kustomize.io/api` similarly. Base + overlays resolved per-environment.

**ArgoCD.** Read-only access to `Application` and `ApplicationSet` CRDs via the in-cluster agent. We extract which repo/path backs each workload.

**Failure mode.** Parse failures are non-fatal. The PR comment says: *"Optiqor couldn't parse this chart. Supported: standard Helm 3, sub-charts, common templating. Not yet: [specific feature]. Tracked at [issue]."* Never crash, never silently skip.

**Operator-managed workloads.** Detected via `ownerReferences` on the Pod/ReplicaSet. When a workload is owned by a CRD (e.g., `Prometheus` from prometheus-operator), Apply Fix is disabled with an explanation that the CRD instance is the correct place to edit.

---

## 6. The Agent Engine

### 6.1 Pipeline Per Finding

```
Finding classified (deterministic) 
  → Contextualization (Postgres + Prometheus + service graph)
    → Prompt construction (structured, stripped of customer comments)
      → LLM call (Sonnet primary, Haiku for simple cases, fallback to OpenAI)
        → Output validation (JSON schema, Helm schema, kubeconform, helm template)
          → Confidence scoring (rule-based bands in Year 1)
            → PR Writer (if High band) or queue for human review (Medium)
```

All of this runs as a Temporal workflow. Idempotent, resumable on crash, with explicit timeouts at each step.

**The LLM-no-decision invariant.** This is the architectural contract codified by ADR-0007: **the LLM produces prose, not values.** The pipeline above puts the LLM call between deterministic classification and deterministic post-validation precisely to enforce this:

- Findings are **classified** by deterministic rules (cost detector library + agent-mode statistical signals). The LLM does not decide which finding fires or how severe it is.
- Values are **computed** by the methodology library (`internal/methodology/`, per ADR-0006). The LLM does not decide what CPU request to recommend, what replica count to set, or what cost to project. The values exist before the LLM call; the LLM is given them as inputs.
- **Diffs are generated by the LLM** because turning structured methodology output into well-formatted Helm YAML is a prose-shaped problem (preserve comments, mirror indentation style, respect anchors, etc.). The LLM's role is *formatting and explanation*, not deciding what to change.
- **Diffs are validated post-LLM** by deterministic gates (JSON schema, Helm values.schema.json, `kubeconform`, helm template, and the "did this modify anything outside the allowed set of keys" check). If the LLM hallucinates a value change, the validator rejects the diff and the workflow retries with a stricter prompt or escalates to human review.

If the LLM is unreachable, the pipeline degrades gracefully: methodology computes the values, a template-based prose generator produces a less-polished PR description, and the PR opens anyway. **No customer decision waits on an LLM.** This is the structural answer to "what happens when GPT-5 ships" — nothing changes about what Optiqor decides; the explanations get marginally better.

### 6.2 Model Strategy

| Task | Model | Rationale |
|------|-------|-----------|
| Finding classification | Deterministic rules, no LLM | Cheap, predictable, easy to test |
| Enrichment (parsing unstructured signals, e.g. operator docs) | Claude Haiku | Fast, cheap |
| Helm values diff generation | Claude Sonnet | Production quality for structured edits |
| Complex refactors (sub-chart changes, ApplicationSet updates) | Claude Opus | Rare (<5% of cases); expensive; escalated only when Sonnet flagged low confidence |
| Output validator (separate LLM call to independently verify) | Claude Haiku | Redundant check; catches prompt injection |

**Fallback abstraction.** The LLM interface is an internal `agent.LLM` type with Anthropic as the default implementation. OpenAI is a drop-in fallback. The application layer doesn't care which. Tested weekly in staging.

### 6.3 Prompt Construction (Defense-in-Depth)

Prompt injection is a real threat because customer Helm values contain arbitrary content, including comments. Four layers of defense:

**Layer 1: Input sanitization.** Before any content reaches the LLM prompt:
- Strip all comments from YAML
- Strip all string values that match a detection pattern for injection (`ignore previous`, `system:`, `you are now`, etc.)
- Limit content to structured data only (numeric and enum fields)
- Fail closed: if sanitization is ambiguous, don't call the LLM — ask human

**Layer 2: Structured prompts.** Never concatenate customer data into a free-form prompt. Use a structured template where customer data populates named fields with clear boundaries.

**Layer 3: Strict JSON output with a second-pass validator.** The primary LLM call returns JSON matching a strict schema. A second LLM call (different model) reviews the output and answers: *"Does this diff only modify resource-related fields, with values in a safe range?"* If no, reject.

**Layer 4: Deterministic post-validators.** JSON schema validation, Helm values.schema.json validation, `helm template` rendering, `kubeconform` K8s schema validation, and a custom "did this modify anything outside the allowed set of keys?" check.

Any single failure of layers 1-4 rejects the output with logging. Customer sees: *"Optiqor's automated validation rejected this change. Queued for human review."*

### 6.4 Validation Without Ephemeral Clusters

The previous doc said we'd spin up ephemeral EKS clusters for validation. **This was wrong.** EKS provisioning is 8-12 minutes; our SLA is 30 seconds.

The real validation stack runs locally in our backend, in a hardened sandbox:

1. **`helm template`** — renders the proposed values against the chart. Library call, ~100ms.
2. **`kubeconform`** — validates rendered manifests against K8s API schema. Library call, ~50ms.
3. **`kubectl apply --dry-run=client`** — client-side dry-run catches obvious errors. Local, ~100ms.
4. **Differential analysis** — compare rendered manifests before and after. Confirm only expected fields changed (e.g., only `resources.requests`, not `image` or `command`).
5. **Constraint checking** — requests within 40% headroom of observed P95, limits ≥ requests, replicas in sensible range.

Total backend-side validation time: <500ms. All deterministic. No cluster needed.

#### 6.4.1 The agent round-trip (catches what local validation can't)

Stages 1-5 above run entirely in the backend and catch ~90% of bad diffs. The remaining ~10% are *cluster-specific* admission rejections: a Kyverno policy that requires a specific label, a Gatekeeper constraint, an OPA rule, a custom validating webhook, a PSP/PSS profile binding the customer enabled three months ago and nobody remembered. Backend-side validation cannot predict these — they live in the customer's cluster admission chain.

So Apply Fix gating uses an **agent round-trip** for stage 6:

```
[Backend]                           [Customer cluster — Optiqor agent]
1. Build candidate diff
2. Run stages 1-5 (local sandbox)
   → if any fail, reject locally
3. Build signed validation request:
   {
     workload_ref,
     proposed_manifests,
     methodology_version,
     nonce,
     issued_at,
     signature (Ed25519)
   }
4. Send request via mTLS  ─────────►
                                    5. Verify backend signature
                                    6. Run `kubectl --dry-run=server`
                                       against the live cluster API
                                       (admission webhooks fire here)
                                    7. Build signed result:
                                       {
                                         request_hash,
                                         outcome: pass | reject,
                                         rejection_reason?,
                                         api_version_seen,
                                         signature (Ed25519, agent key)
                                       }
                            ◄────── 8. Return signed result
9. Verify agent signature
10. Verify request_hash matches the
    nonce we issued (replay defense)
11. If outcome == pass: open the PR
    If outcome == reject: log the
    rejection reason, do not open PR,
    surface in the dashboard
```

**Why this works security-wise:**

- The agent has **no write access** to the cluster (per ADR-0008). `kubectl --dry-run=server` is a read-only operation that exercises the admission chain without persisting changes.
- The signed request prevents a compromised backend session from forging "validate this for me" requests at the agent; the agent's signed result prevents a compromised network path from forging "yes, the cluster accepted this."
- The nonce in the request + the nonce echoed in the result prevent replay: an attacker can't reuse a previous "pass" result against a different diff.
- mTLS for the transport is the existing agent ↔ backend channel; no new attack surface.

**Latency:** the round-trip adds ~1-3 seconds to PR opening time on a healthy customer cluster (most of that is admission-webhook execution, which is the customer's own infrastructure, not Optiqor). Fail-closed: if the agent doesn't respond within 10s, the PR is not opened. The dashboard surfaces "validation timeout" with the recommendation queued for retry.

**Why the agent, not a backend-side kubeconfig:**

We could ask the customer for a read-only kubeconfig and run `kubectl --dry-run=server` from the backend. We don't, for three reasons:

1. **Customer's security team objects to kubeconfig handoff** more often than to an agent install. Outbound-only mTLS is easier to approve than inbound network access from a SaaS to a private cluster.
2. **Latency.** Backend ↔ customer cluster is internet round-trip; agent is in-cluster. Admission webhook calls (often 100-500ms each) are local for the agent and trans-WAN for the backend.
3. **Operational fragility.** Customer kubeconfigs rotate; tokens expire; firewalls change. The agent is the customer-controlled side of the trust boundary and handles its own refresh.

Implementation lives in `internal/applyfix/gate/dryrun/` per [optiqor/todo.md](../../todo.md) line 245 — backend-side signed-request issuer; agent-side handler ships as part of the Phase 5 agent watch loop.

### 6.5 Confidence Scoring (Year 1)

Rule-based, weighted, mapped to bands. No numerical display until we've earned it.

```go
type ConfidenceSignal struct {
    Name   string
    Weight float64
    Pass   bool
    Reason string
}

func ComputeConfidence(ctx context.Context, finding *Finding) (Band, []Signal, error) {
    signals := []Signal{
        evalUsageHeadroom(finding),        // weight 0.30
        evalNoOOMKilled(finding),           // weight 0.20
        evalNoSLOBurn(finding),             // weight 0.15
        evalHPAStability(finding),          // weight 0.15
        evalBlastRadius(finding),           // weight 0.10
        evalPatternMatch(finding),          // weight 0.10
    }

    raw := 0.0
    for _, s := range signals {
        if s.Pass {
            raw += s.Weight
        }
    }

    band := Low
    switch {
    case raw >= 0.85:
        band = High
    case raw >= 0.65:
        band = Medium
    }
    return band, signals, nil
}
```

**Band behavior:**
- `High` → auto-open PR (never auto-merge)
- `Medium` → queue for human review, no PR opened
- `Low` → log for learning, no action

**Display rule:** the PR comment always shows the signals — not the raw score. "High — based on: ✓ 30 days of stable usage, ✓ no OOMKilled, ✓ no SLO burn, ✓ HPA stable, ✓ no critical-path deps."

### 6.6 What We Rightsize (Safety Boundaries)

| Workload Kind | Auto-Apply-Fix? | Rationale |
|---------------|-----------------|-----------|
| Deployment (stateless) | ✅ Yes, at High confidence | Can restart freely, HPA handles surges |
| ReplicaSet (unmanaged) | ⚠️ Suggestion only | Rare; usually a signal of something wrong |
| StatefulSet | ❌ Suggestion only, human review required | Pod identity, PVCs, data integrity constraints; memory changes risk data-service OOM |
| DaemonSet | ❌ Excluded | Per-node, affects all nodes simultaneously; chart authors usually tuned them |
| Job / CronJob | ✅ Yes, with run-history analysis | Analyze last 10 successful runs before recommending |
| Operator-managed (CRD-owned) | ❌ Skip with explanation | Operator reconciles to CRD spec; editing Deployment is wrong |
| kube-system, kube-public | ❌ Excluded | Changes here break the cluster |

---

## 7. Cost Engine & CUR-to-Pod Attribution

### 7.1 Why This Is Genuinely Hard

AWS Cost and Usage Reports (CUR) give you costs per EC2 instance, EBS volume, NAT gateway, ALB, etc. CUR does NOT say "pod X cost $437 this month." To issue a Receipt, we must attribute node-level costs down to specific pods. Kubecost spent 5 years on this. We'll be direct about our approach and its limits.

### 7.2 Year-1 Attribution Model (Hybrid Requests + Usage)

For each (hour × node):

```
For each pod scheduled on that node during that hour:
  cpu_share   = (α · pod_requested_cpu    + (1-α) · pod_actual_cpu)
                 / sum_over_pods_on_node
  mem_share   = (α · pod_requested_memory + (1-α) · pod_actual_memory)
                 / sum_over_pods_on_node

  pod_node_cost = node_hour_cost · (cpu_share + mem_share) / 2

Where α = 0.6 (requests-weighted slightly favored)

Node-level idle capacity (unscheduled time) is distributed per namespace policy:
  - With namespace labels: proportional to requests-weighted share
  - Without labels: marked "unallocated_overhead" (shown to customer)

Daily aggregation rolls up hour-level attributions → pod-day cost → workload-day cost → Receipt window.
```

**What this captures well:**
- Steady-state workloads (most of them)
- Requests-heavy environments where pods sit on nodes predictably
- Per-workload trend analysis

**What this captures imperfectly:**
- Spot instance interruptions — we extend the model with explicit interruption windowing (`node_lifetimes.interruption_at`, see §7.3) to attribute partial windows rather than billed hours
- Cross-account Savings Plans — we apply blended SP rate per usage tier
- Network data-transfer costs (cluster-level overhead, not pod-attributed in Year 1)
- GPU attribution (Year 2 — requires nvidia-dcgm-exporter)

**What Receipts display:**
```
Methodology:            Optiqor hybrid_v1
Attribution confidence: 88% (node-hours where all pods had labels)
Not attributed:         $180 of $19,120 (0.9%) — shared cluster overhead
Full methodology:       optiqor.dev/methodology/hybrid-v1
```

Honesty about what's attributed vs. what's overhead is how we earn trust. The methodology URL is public and versioned; the Receipt is Ed25519-signed and reproducible from the public spec.

### 7.3 Karpenter and Autoscaler Dynamics

Karpenter-managed nodes are ephemeral. We handle this with a "node-lifetime window" approach:

```sql
-- Node lifecycle tracking
CREATE TABLE node_lifetimes (
    tenant_id UUID NOT NULL,
    cluster_id UUID NOT NULL,
    node_name TEXT NOT NULL,
    instance_type TEXT NOT NULL,
    capacity_type TEXT,  -- on_demand, spot
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    zone TEXT,
    cost_per_hour_usd NUMERIC(10,5)
);
```

Attribution runs per (node × lifetime window). Nodes that lived for 3 hours get 3 hours of cost attribution to the pods that ran on them during that window. Spot interruption is a distinct event — we mark the window with `capacity_type = 'spot'` and `interruption_at` if relevant.

### 7.4 Receipt Verification Workflow

30 days after a Optiqor PR merges, a Temporal workflow fires:

```
1. Fetch CUR snapshot for (tenant × affected_namespaces × [merge_date - 30d, merge_date + 30d])
2. Aggregate cost_attributions for affected workloads, pre-merge and post-merge
3. Compute actual delta = post_merge_avg - pre_merge_avg (USD/month)
4. Compare with predicted_cost_delta from PR creation
5. Generate Ed25519 signature over (predicted, actual, methodology_version, timestamp, pr_id)
6. Store receipt in Postgres + S3
7. Post receipt as PR comment
8. Emit metric: receipt_accuracy_pct = min(predicted, actual) / max(predicted, actual)
```

Signing keys rotate quarterly. Old public keys remain available on the verification endpoint. Any third party can verify a receipt by fetching `optiqor.dev/verify/{receipt_id}` and checking the signature against the published public keys.

---

## 8. Auto-Rollback Guard

### 8.1 The Real Problem: Statistical Rigor (and the structural moat)

This section is the most architecturally load-bearing part of Optiqor. It's the one component the rest of the K8s ecosystem **structurally cannot** ship — and that's why it's the moat. Engineers building this section should read it with that lens.

**Why the customer's existing autoscalers don't close this loop.** When an Optiqor fix merges and goes wrong, the customer's HPA / VPA / Karpenter all react — but they react to symptoms, not causes:

- HPA sees "pod failing" → scales replicas up. Masks the bug. Bill goes up.
- VPA in Auto mode sees "OOMKilled" → increases requests. Papers over the cause.
- Karpenter sees "pods unschedulable" → provisions new nodes. Pays for the masking.

None of them knows the regression started 4 hours ago, correlated with PR #1247, opened by Optiqor. None of them has a pre-merge baseline to compare against. They're stateless reactors. The K8s primitives have no concept of *change attribution*; auto-rollback fundamentally needs change attribution.

**Why this is a moat, not a feature.** Cast AI, ScaleOps, and Sedai could write similar math, but (a) their implementations are closed-source — customers cannot audit the rollback decision; (b) none of them sit in the PR layer, so they have no commit SHA / merge timestamp to anchor the pre/post comparison; (c) Kubecost is a dashboard, not a controller — they have no rollback story at all. The math itself (Box-Cox transform on lognormal cost + STL decomposition for daily/weekly seasonality + PELT change-point detection for locality) is mature signal processing, but applying it to K8s deployment regression with <2% false-positive rate is a 6-12 month engineering effort. Once we ship it, the gap stays open. See [business_strategy.md §8.4 moat #2](../strategy/business_strategy.md) for the positioning angle; the rest of §8 is the engineering spec.

**Naive rollback triggers fire constantly on normal variance.** Getting this right is hard signal-processing work, not a feature. Phased rollout protects customers from bad rollback decisions.

### 8.2 Phase Progression

**Phase 1 (Months 9–12) — Observe & Alert Only**
- Monitor 5 signals for 7 days after merge (see 8.3)
- On breach: post a comment on the original PR, send Slack alert
- **No rollback PR opened.** Pure observation mode.
- Goal: gather false-positive statistics before committing to auto-action.

**Phase 2 (Months 12–18) — Rollback PR, Human Merge**
- On confirmed breach: open a rollback PR, page on-call
- Human clicks merge within 4-hour SLA; escalation if missed
- Available on Team+ tier.

**Phase 3 (Months 18+) — Opt-in Auto-Merge for Non-Critical**
- Customer-configurable: certain resource classes (batch, dev namespaces) can auto-merge rollback PRs
- Critical paths (payments, auth, customer-facing APIs) always require human approval
- Enterprise tier only, with financial SLA.

At every phase, **Optiqor never auto-merges PRs against customer-marked critical paths.** This is absolute.

### 8.3 Signals Monitored (5 Sources, 7-Day Window)

| Signal | Detection Method | Threshold |
|--------|------------------|-----------|
| Cost deviation | Per-service daily cost vs. 28-day baseline; Box-Cox transformation for lognormal; flag if z-score > 2.5 for 2 consecutive days | Statistical |
| OOMKilled events | Absolute count in 24h window | Any non-zero on workload within 24h post-merge |
| CrashLoopBackOff | Absolute count | Any pod transitions on affected workload within 24h |
| P99 latency | If customer provides APM integration: compare to 14-day baseline with daily seasonality controls (simple STL decomposition) | 1.5× baseline p99 for 15+ min |
| SLO burn-rate | Customer-defined Prometheus alerts | Alert fires on affected workload |

**Why Box-Cox for cost:** raw cost is lognormal, not normal. A Box-Cox transform produces a symmetric distribution where z-score is meaningful. Naive 3σ on raw costs produces ~10% false-positive rate; on Box-Cox transformed data, <2%.

**Minimum-sample guard:** we never alert on workloads with <14 days of baseline history. Noisy small-workload signals are ignored until baseline stabilizes.

### 8.4 Breach Confirmation

A signal breach alone is not enough. Before any rollback action:

1. **Re-check in 5 min.** Transient spike? Drop.
2. **Correlation with deployment.** The breach must correlate temporally with the merged PR's deployment — if the change wasn't deployed yet (ArgoCD hasn't synced), no rollback.
3. **Secondary signal.** For Phase 2+, require two independent signals (e.g., cost spike AND OOMKilled) before opening a rollback PR.
4. **Business-hours window (Enterprise default).** Rollback only during business hours unless customer opts into 24/7.
5. **Customer opt-out per workload.** Customers mark workloads as "never auto-rollback" — always honored.

### 8.5 Rollback PR Generation

```
Trigger: confirmed breach on workload W from PR P
Action:
  1. Compute inverse diff (revert the commit that caused the deployment)
  2. Open "🚨 Optiqor ROLLBACK" PR with clear reasoning:
     - "PR #P merged 4 days ago caused [specific signals]"
     - "Reverting these changes to restore pre-merge state"
     - "If you want to keep the change, close this PR"
  3. Tag author of original PR + platform on-call
  4. Send Slack/PagerDuty notifications
  5. Log to receipts table as "rollback_event"
```

### 8.6 False-Positive Reporting Loop

Customers can mark any rollback alert as "false positive" with one click. This feeds back into our threshold tuning and is reported in our weekly metrics as `auto_rollback_fp_rate`. Target: <5%. Above 10%, we freeze rollback for that customer and investigate.

### 8.7 Statistical math — code shape and interface seam

§8.3 names the math (Box-Cox, STL decomposition, change-point); this section pins how that math lives in code so the watchdog state machine and the math stay independently testable.

**Package layout** (Phase 7, per [optiqor/todo.md](../../todo.md)):

```
internal/methodology/rollback/
├── doc.go
├── stats.go           — Stats interface + struct definitions
├── boxcox.go          — Box-Cox transform: pure functions, no I/O
├── boxcox_test.go     — Round-trip + lambda-estimation + golden tests
├── stl.go             — Seasonal-Trend-Loess decomposition (24h + 168h)
├── stl_test.go        — Synthetic-series tests with known seasonality
├── changepoint.go     — CUSUM / Pruned Exact Linear Time change-point
├── changepoint_test.go
└── score.go           — Combines transform + decomposition + change-point
                         into a single PreMergePost comparison; returns a
                         signed deviation score the watchdog state machine
                         consumes
```

Rules from ADR-0006 apply: pure functions, no I/O, no clock reads, no random map iteration. The math takes time-series in, returns deviation scores out. Caller (the watchdog workflow) handles fetching the Prometheus rollup, persisting results, and posting notifications.

**Interface seam:**

```go
// Stats is the rollback math the watchdog state machine consumes.
// Phase-6 ships SimpleStats (z-score on raw values); Phase-7 swaps in
// FullStats (Box-Cox + STL + change-point) without touching the watchdog.
type Stats interface {
    // Score compares pre-merge baseline against post-merge observed and
    // returns a deviation in [0, +∞) where 0 means "indistinguishable"
    // and 1+ means "statistically significant breach at the configured
    // threshold." The state machine treats >= 1.0 as a breach signal.
    Score(baseline, observed []Sample, kind SignalKind) (DeviationScore, error)
}

type DeviationScore struct {
    Value             float64  // 0..+∞; ≥1.0 = breach
    Confidence        float64  // 0..1; how much we trust the signal given sample count, seasonality match, change-point clarity
    TransformApplied  string   // "boxcox" | "none"
    SeasonalityModel  string   // "stl_daily" | "stl_weekly" | "none"
    ChangePointAt     *time.Time
    MinSamples        int      // input length actually used
}
```

**Why the math lives in `internal/methodology/rollback/` not `internal/rollback/`:** the watchdog state machine in `internal/rollback/watchdog.go` is the *decision* (continue / rollback / close window); the math is the *signal*. Splitting them honors the LLM-no-decision invariant's structural cousin: math here, decisions there. The same `Stats` interface gets stubbed (`SandboxStats` returns a fixed deviation for any input) so the state machine can be tested without the math, and the math can be tested without the state machine.

**Math choices and why:**

1. **Box-Cox transform** — Pre-merge cost and latency time-series are lognormal (positive, right-skewed, heavy upper tail). Applying a Box-Cox transform produces an approximately normal distribution where z-scores have meaning. Lambda is estimated per workload from the pre-merge baseline window; lambda=0 collapses to log-transform. Naive z-score on raw values produces ~10% false-positive rate at the 2.5σ threshold; Box-Cox-transformed z-score produces <2% on our pilot data. Reference: NIST e-Handbook 1.3.3.6 "Box-Cox normality plot."

2. **STL decomposition (Seasonal-Trend-Loess)** — Real workloads have daily and weekly seasonality (the API server is busier at 2pm than 2am; Mondays differ from Sundays). Comparing raw post-merge to raw pre-merge confuses seasonal variation with deployment-caused regression. STL separates the signal into trend + seasonal + residual; we compare the residual component pre/post, which is what regression actually moves. Window choices: 24h period for daily, 168h for weekly; both run by default and the stronger of the two is picked per signal.

3. **Change-point detection (PELT)** — Even with Box-Cox + STL, normal variance produces occasional 2.5σ spikes that aren't deployment-caused. A change-point algorithm asks "did the underlying distribution shift, and if so when?" If the change-point is within 2 hours of the deployment time, that's a strong signal it's the deployment's fault. If the change-point is hours/days earlier, the deployment isn't the cause. PELT (Pruned Exact Linear Time) is the standard for this; gonum / robfig/cron have Go ports.

**Math libraries:**

- `gonum.org/v1/gonum/stat` — mean, variance, percentiles, Box-Cox helpers
- `gonum.org/v1/gonum/fourier` — FFT for autocorrelation in the classifier (§classify) and STL
- No Python dependency. The Go ecosystem covers the math; ADR-0006 forbids a second language for methodology.

**Phase progression** (matches §8.2):

- **Phase 5 (Months 9-12)** — `SimpleStats` ships: bounds-vs-snapshot comparison, no transform, no decomposition. Watchdog state machine is wired and observable. Auto-rollback in observe-only mode.
- **Phase 6 (Months 12-15)** — Box-Cox transform lands. Phase-2 rollback PR generation enabled.
- **Phase 7 (Months 15-18)** — STL + change-point detection. Phase-3 opt-in auto-merge for non-critical paths.

Each phase's math is a drop-in replacement behind the `Stats` interface; the watchdog state machine, the Temporal workflow, and the PR generator do not change.

**False-positive budget per phase:**

| Phase | Math | Target FP rate | Action if exceeded |
|---|---|---|---|
| 5 (SimpleStats) | z-score on raw | ≤ 15% | Observe-only; FPs cost nothing |
| 6 (Box-Cox) | z-score on transformed | ≤ 5% | Rollback PRs gated on FP rate per tenant |
| 7 (STL + change-point) | residual + locality | ≤ 2% | Opt-in auto-merge gated on FP rate per workload class |

The `auto_rollback_fp_rate` metric from §8.6 is what's measured against these targets.

---

## 9. Cost Spike → PR Mapping

### 9.1 The CFO Feature, Engineered Properly

Weekly job: detect anomalies in (tenant × namespace × day) cost. For each anomaly, return candidate PRs ranked by causal likelihood.

### 9.2 Detection

Hourly job using seasonal anomaly detection:

```python
# Conceptual — actual is Go with gonum/stat
decompose(namespace_cost_timeseries, period=7_days) -> trend + seasonal + residual
flag_anomaly if abs(residual) > 3 * median_absolute_deviation(historical_residuals)
```

STL decomposition handles weekly seasonality (batch workloads, dev traffic cycles). Median Absolute Deviation is robust to outliers unlike standard deviation.

### 9.3 Causal Attribution

Given an anomaly starting on date `T`, rank candidate PRs:

```sql
WITH candidate_prs AS (
  SELECT pr.id, pr.repo, pr.number, pr.merged_at,
         pr.predicted_cost_delta_usd_month,
         (EXTRACT(EPOCH FROM (T - pr.merged_at)) / 86400) AS days_before
  FROM pull_requests pr
  JOIN iac_files f ON f.id = ANY(pr.modified_iac_file_ids)
  JOIN workloads w ON w.source_iac_path = f.path
  WHERE pr.merged_at BETWEEN T - INTERVAL '21 days' AND T
    AND w.namespace = $1
    AND pr.tenant_id = current_tenant_id()
)
SELECT id, repo, number, merged_at, predicted_cost_delta_usd_month,
  (CASE 
    WHEN days_before < 2 THEN 1.0
    WHEN days_before < 7 THEN 0.8
    WHEN days_before < 14 THEN 0.5
    ELSE 0.2
  END) * 
  (CASE 
    WHEN predicted_cost_delta_usd_month > 0 THEN 1.5
    ELSE 0.5
  END) AS causal_score
FROM candidate_prs
ORDER BY causal_score DESC
LIMIT 5;
```

For Team+ tier, we go further: run a small regression where anomaly magnitude is the target and candidate-PR predicted-delta is a feature. This catches the "multiple PRs added up to a big change" case.

### 9.4 Presentation

Weekly Slack digest, and on-demand via `/Optiqor spike`:

```
📈 Cost anomaly — checkout namespace
Period: April 8-15 · Increase: +$14,200 week-over-week

🎯 Likely cause: PR #4821 (@sarah, merged April 2)
   "Scale checkout workers for Black Friday prep"

Breakdown of attributable increase:
• CPU requests increased 2× across 6 services → +$8,000
• Replica count 4→8 on 3 services         → +$6,200

Optiqor predicted +$13,100; actual +$14,200 (within 8%).

Secondary contributors (this week):
• PR #4847 (@raj, Apr 8): Redis m5.2xlarge upgrade  → +$1,700
• Organic growth:                                     +$500
```

---

## 10. Security Architecture

### 10.1 Trust Model

Optiqor reads customer IaC, reads customer cluster state, reads customer billing data. A breach is catastrophic. Security is Day-1 concern, not a Year-2 roadmap item.

### 10.2 Multi-Tenancy Isolation

| Layer | Mechanism |
|-------|-----------|
| Database | Row-level security policies on every table |
| API | Every handler opens a transaction and calls `set_config('app.tenant_id', <uuid>, true)` before any query (via `internal/platform/db`); RLS policies read through `current_tenant_id()` |
| LLM workers | Per-tenant prompt contexts; no cross-tenant data in any single inference |
| Redis | Keys prefixed with `t:<tenant_id>:`; ACL-enforced namespaces |
| S3 | Per-tenant prefix; IAM policies restrict access by prefix |
| Logs | All logs structured with `tenant_id`; tenant_id redaction for any logs shown in dashboards accessible across tenants |
| Worker queues (Temporal) | Task queues partitioned by tenant cohort, not shared global queue |

### 10.3 Credentials

**Customer cloud credentials are never stored.** We use STS AssumeRole with external ID for AWS CUR access; token TTL is 1 hour, refreshed on demand.

**GitHub OAuth tokens** are encrypted at rest using AWS KMS with per-tenant keys. Rotation is automatic via GitHub App installation refresh.

**Agent-to-SaaS authentication** uses short-lived JWTs (15 min TTL) with key rotation every 24 hours. The JWT is derived from a customer-specific bootstrap token + mTLS client certificate.

### 10.4 Network

- All internal services run in private subnets
- NAT Gateway for egress only
- Public ingress through CloudFront → WAF → ALB → app
- Service-to-service calls inside our EKS cluster use mTLS via Istio (Year 2) or via TLS at the app layer (Year 1)
- No service has direct internet access except the LLM proxy and the AWS CUR fetcher

### 10.5 Agent Security (the thing customers audit)

- Open source, Apache 2.0, `github.com/optiqor/agent`
- Binary signed with Sigstore; SBOM published per release
- Runs as non-root UID 1000
- `readOnlyRootFilesystem: true`
- No hostNetwork, no hostPath, no privilegedContainers
- `allowPrivilegeEscalation: false`
- All capabilities dropped
- Egress allowlist: only `ingest.optiqor.dev` over port 443
- Customer can inspect every outbound request via their egress proxy

### 10.6 Application Security

- Annual third-party penetration test (Month 9 for the first one)
- Bug bounty program from Month 12 (HackerOne or internal)
- Every PR triggers `gosec`, `govulncheck`, `staticcheck`, and dependency vulnerability scanning via `trivy`
- Secrets scanning via `gitleaks` in pre-commit and CI
- OWASP ASVS Level 2 compliance by Month 12
- SOC 2 Type 1 kickoff Month 9, certified by Month 15
- SOC 2 Type 2 by Month 21

### 10.7 Prompt Injection & Adversarial Input

Customer content enters our LLM prompts. A malicious or compromised customer PR could embed injection. Defenses as specified in Section 6.3:
1. Input sanitization (strip comments, detect injection patterns)
2. Structured prompts (no free-form concatenation)
3. Strict JSON output with independent validator
4. Deterministic post-validators

All prompts are versioned, logged (inputs redacted), and A/B tested before rollout. A new prompt ships behind a feature flag to a 5% cohort, runs for 72 hours, and rolls out only if metrics (cost-per-PR, false-positive rate, customer-visible error rate) hold.

### 10.8 Data Handling and Compliance

- PII detection in ingestion: resource labels, annotations, and env-var names (not values) are scanned for obvious PII patterns (email addresses, employee IDs). Matches are redacted before storage.
- Audit logs are immutable, append-only, in Postgres with a monthly export to S3 with Object Lock + Glacier. 7-year retention for Enterprise, 1-year for others.
- GDPR: EU region deployment Month 18. Until then, EU customers sign DPA with us-east-1 data residency waiver, or wait.
- HIPAA BAA available Month 30 (Year 2 Q4).

---

## 11. Infrastructure & DevOps

### 11.1 Environments

| Env | Infra | Purpose |
|-----|-------|---------|
| `dev` | `kind` + Tilt on engineer laptops | Fast iteration |
| `staging` | Small EKS cluster in `us-east-2` | Production-identical, all tests run here |
| `prod` | Multi-AZ EKS in `us-east-1` | Customer-facing |
| `prod-eu` (Year 2) | Multi-AZ EKS in `eu-west-1` | EU data residency |

### 11.2 Deployment Pipeline

```
git push → GitHub Actions:
  → test (unit, integration against real Postgres/Redis via testcontainers)
  → lint (gosec, govulncheck, staticcheck)
  → build container (distroless base)
  → scan container (trivy)
  → sign with cosign
  → push to ECR
  → update Helm chart tag in our ArgoCD repo
ArgoCD detects change → rolls out to staging → runs smoke tests
Manual promotion → prod (Year 1); automated with canaries (Year 2)
```

**Change freezes:** Fridays after 2pm local, day before Anthropic/AWS major releases.

**Rollback:** ArgoCD supports instant revert to the previous deployment. We target <5 min MTTR for any deployment that breaks the product.

### 11.3 Observability

Everything ships metrics, logs, and traces from commit #1.

| Layer | Tool | What we watch |
|-------|------|---------------|
| Metrics | Prometheus + Grafana | Per-service latency, error rate, throughput; LLM cost per PR; Apply Fix success rate; Receipt accuracy |
| Logs | Loki | Structured JSON logs; tenant_id in every line; trace_id for cross-service correlation |
| Traces | OpenTelemetry → Tempo | End-to-end: webhook receipt → parse → LLM → validate → PR post |
| Errors | Sentry | Code-level error tracking with release tracking |
| Product | Amplitude or PostHog | Funnel: install → first PR → first Apply Fix → first Receipt |
| LLM cost | Custom dashboard | Per-customer, per-feature, per-model daily spend; anomaly alerts |
| Infra cost | AWS Cost Explorer + custom | Cost per merged PR (top business metric) |

**SLO dashboards** per customer (Enterprise): API uptime, PR comment latency, Apply Fix success rate, Receipt accuracy.

**Alerting** via PagerDuty with three tiers:
- P0 (wake up): customer data leak, total outage
- P1 (within 30 min): degraded service for >10% of customers
- P2 (next business day): individual customer issues, cost anomalies

### 11.4 Service-Level Objectives

| Metric | Year 1 Target | Year 2 Target | Measurement |
|--------|---------------|---------------|-------------|
| API availability | 99.5% | 99.9% | Calendar month |
| PR comment latency (p95) | 45 seconds | 20 seconds | From webhook receipt to posted comment |
| PR comment latency (p99) | 90 seconds | 60 seconds | Same |
| Apply Fix generation success rate | 85% | 92% | Fix generated without hitting retry/fallback |
| Receipt accuracy | within 20% | within 15% | `min(predicted, actual) / max(predicted, actual)` averaged |
| Auto-Rollback false-positive rate | <10% (Phase 1) | <5% (Phase 2) | Customer-confirmed false positives / total alerts |
| LLM cost per merged PR | <$0.35 | <$0.20 | Direct cost from provider, pro-rated to PR |
| Data loss | Zero | Zero | Non-negotiable; RPO = 0 for customer-facing data |

### 11.5 Disaster Recovery

**RPO (Recovery Point Objective):**
- Customer-facing data: 0 (synchronous replication to Multi-AZ standby)
- Analytics / time-series: 1 hour (hourly snapshots to S3)

**RTO (Recovery Time Objective):**
- Partial degradation: <5 min (AZ failover)
- Full regional failure: <4 hours (restore from snapshot to warm standby region, Year 2)

**Backup cadence:**
- Postgres: continuous WAL archiving to S3, daily full snapshots, point-in-time recovery tested quarterly
- Redis: hourly snapshots (not source of truth — cache only)
- S3: cross-region replication for receipts and audit logs

**Chaos testing:** monthly (from Month 6). Kill a pod, kill a database replica, fail an AZ. Measure time to detection and recovery.

### 11.6 In-Cluster Agent Footprint

| Cluster Size | CPU Request | CPU Limit | Memory Request | Memory Limit | Egress / hour |
|-------------|-------------|-----------|----------------|--------------|----------------|
| Small (<100 pods) | 50m | 200m | 128Mi | 256Mi | <2 MB |
| Medium (100-1000 pods) | 100m | 500m | 256Mi | 512Mi | <10 MB |
| Large (1000+ pods) | 250m | 1000m | 512Mi | 1Gi | <50 MB |

Stateless Go binary. Polling at 15-min intervals (configurable). Prometheus queries execute on customer's Prometheus (we don't replicate). gzip on all egress.

### 11.7 No-Agent Mode Architecture

For customers who cannot install the agent:

- Customer provides: read-only kubeconfig for a Optiqor ServiceAccount (cluster-scoped `get`, `list`, `watch`), Prometheus remote-read endpoint, AWS CUR S3 bucket IAM role
- Polling worker runs on our side at 60-min intervals (vs 15 for agent)
- All network flows initiated from Optiqor, outbound-only from their perspective
- Signal richness ~60% of agent mode (no real-time events)
- PR comment latency ~90s instead of 30s

Trade-off is explicit: slower, less rich, but zero customer-side footprint.

---

## 12. AI / LLM Cost Management

### 12.1 Budget and Discipline

Target: <$0.35 cost per merged PR by Month 6, <$0.20 by Month 18.

**Composition (typical Helm rightsizing, Sonnet):**
- Classification (deterministic): $0.00
- Enrichment (Haiku): ~$0.02
- Generation (Sonnet): ~$0.18
- Validation (Haiku, second-pass): ~$0.03
- Total: ~$0.23

### 12.2 Cost Control Tactics

**Prompt caching.** Anthropic's prompt caching cuts input cost 90% on cache hits. We structure prompts so the system prompt + pattern library is cached (static) and only the customer-specific section varies. Target cache hit rate: 50% Year 1, 75% Year 2.

**Model routing.** Deterministic classifier → Haiku for enrichment → Sonnet only for generation → Opus only when Sonnet's low-confidence escalation fires (<5% of cases).

**Batch inference.** When we scan a customer's cluster and find 10 overprovisioned workloads, we don't run 10 independent prompts. We batch related findings into a single prompt and generate multiple diffs in one pass.

**Fallback to deterministic.** For trivial fixes (e.g., setting `runAsNonRoot: true`), skip the LLM entirely and apply a templated fix.

**Fine-tuning (Month 18+).** Once we have 50K+ merged Optiqor PRs with verified outcomes, fine-tune a small open-weight model on them. For the common 80% of Helm values patterns, the fine-tuned model beats Sonnet at a tenth the cost.

### 12.3 LLM Cost Observability

Every LLM call logs: `tenant_id`, `feature`, `model`, `input_tokens`, `output_tokens`, `cache_hit`, `cost_usd`, `latency_ms`, `retry_count`. These flow to a dedicated "LLM Cost" dashboard. Daily anomaly alerts fire if cost per PR deviates >2σ from recent baseline.

---

## 13. The First 90 Days (Engineering Plan)

Concrete weekly targets. This is the build that produces seed-ready demo artifacts.

**Weeks 1–2 — Foundation**
- Terraform our AWS infrastructure (VPC, EKS, RDS Postgres, ElastiCache Redis, S3 buckets)
- GitHub Actions CI/CD → ECR → ArgoCD deployment to staging cluster
- Observability stack live (Prometheus, Grafana, Loki, Sentry, OTEL)
- Postgres schema + migrations via sqlc + golang-migrate
- GitHub OAuth + GitHub App registration working end-to-end
- **Milestone:** empty `Optiqor-backend` deploys to staging automatically; observability dashboards populated

**Weeks 3–4 — Ingestion & Sandbox MVP**
- In-cluster agent (Go): watch Deployments/StatefulSets/HPAs via informers, ship to ingest endpoint
- Prometheus query runner in backend
- Helm parser (values.yaml + common templating patterns)
- **Public sandbox** (`optiqor.dev/sandbox`): accepts pasted Helm values.yaml, returns synthetic cost analysis using AWS generic pricing (no Prometheus data needed)
- Dogfood: install agent in our own EKS cluster
- **Milestone:** any public visitor can paste a Helm chart and see cost analysis in <10s

**Weeks 5–6 — Agent Engine v0 & CLI**
- Deterministic classifiers for OverprovisionedCPU, OverprovisionedMemory, MissingHPA
- Sonnet-backed Helm values diff generation (with structured prompts + strict JSON output + validator)
- Multi-layer validation pipeline (helm template + kubeconform + diff analysis)
- Confidence band computation (rules from 6.5)
- CLI (`npx @optiqor/cli`) wrapping the sandbox backend — our Hacker News artifact
- **Milestone:** Apply Fix PR generated end-to-end on our own dogfood cluster; CLI published on npm

**Weeks 7–8 — PR Writer, Apply Fix Flow, Kustomize**
- GitHub App PR composition with embedded Optiqor metadata
- Apply Fix signed-token flow (Ed25519, 24h TTL, tenant-scoped)
- Full PR comment template with Confidence band, signals, Engineer Impact
- Kustomize parser
- **Milestone:** first real Apply Fix merged on our own repo; <30s webhook→comment latency consistently

**Weeks 9–10 — Design Partner #1 + Slack**
- Helm chart polished for customer install; bootstrap token flow hardened
- First design partner onboarded (founder's network), embedded in their Slack for rapid feedback
- Slack daily digest + `/Optiqor` slash commands v0
- Skeptic Mode (passive-observer) deployment toggle
- **Milestone:** design partner #1 has Apply Fix PRs landing on their real Helm charts; Slack digest delivering daily

**Weeks 11–12 — Receipts, Cost Spike, Partners #2-3**
- AWS CUR ingestion (Athena-backed query layer over CUR S3 bucket)
- Cost attribution model (hybrid_v1) running daily
- Receipt generation + Ed25519 signing + public verification endpoint
- Cost Spike → PR Mapping v0 (STL decomposition on namespace costs, candidate PR ranking)
- Design Partners #2 and #3 onboarded
- First verified Receipt delivered ($500+/mo confirmed against real CUR)
- Demo video recorded

**Day 90 Exit Criteria:**
- Sandbox live, <3s p95 response time, >100 real visitors used it
- CLI on npm with >50 organic installs
- 3 design partners with Apply Fix PRs landing on real Helm charts
- 1+ verified Receipt with <20% prediction error
- End-to-end pipeline: PR webhook → analysis → Apply Fix → merge → Receipt (30-day)
- Confidence bands (High/Medium/Low) display with real supporting signals
- SLOs met: API 99.5% uptime, p95 PR comment latency <60s
- Cost per PR <$0.40
- Clean staging / prod separation, rollback drills passing
- Seed fundraising deck with live demo

---

## 14. Roadmap Beyond Day 90

**Months 3–6 — Public Beta**
- Auto-Rollback Guard Phase 1 (observe-only)
- 15 detectors across cost and basic security
- ArgoCD Application integration
- Free tier + self-serve Team billing
- First paying Team customers
- SOC 2 Type 1 kickoff

**Months 6–9 — Scale**
- Auto-Rollback Guard Phase 2 (rollback PR + human merge)
- 25 detectors
- Engineer Impact scoring
- KubeCon EU launch (keynote submission)
- 50+ paying teams, first enterprise pilots

**Months 9–12 — Enterprise Foundation**
- SOC 2 Type 1 certified
- SSO / SAML, audit log export
- No-Agent Mode GA
- 5 enterprise customers at $100K+ ACV
- Close Seed round

**Year 2**
- GKE + AKS support (Q1)
- GitLab integration (Q2)
- Terraform for K8s-adjacent AWS resources (Q2-Q3)
- Confidence Score v2 (ML-augmented, fine-tuned small models)
- SOC 2 Type 2
- EU region deployment
- Flux CD support

**Year 3+**
- Full Terraform cost coverage (head-to-head with Infracost from K8s position)
- AI/LLM infrastructure cost analysis
- Carbon-per-PR (CSRD compliance)
- Public detector SDK
- SaaS cost detectors (Snowflake, Datadog, Databricks)

---

## 15. Engineering Metrics (Reviewed Weekly)

| Metric | Target (Year 1) | Why |
|--------|-----------------|-----|
| Cost per merged PR | <$0.35 | Gross margin |
| PR comment latency p95 | <45s | User experience |
| Apply Fix success rate | >85% | Flagship reliability |
| Confidence band calibration error | <15% | Trust validity |
| Receipt prediction accuracy | <20% variance | Customer trust |
| Auto-Rollback false-positive rate (Phase 1) | <10% | Path to Phase 2 |
| Postgres query p99 | <200ms | Internal speed |
| Weekly active customers | growth >10%/month | Adoption |
| Time to first Apply Fix merged | <24h post-install | Activation |
| LLM cache hit rate | >50% | Cost efficiency |
| Sandbox→GitHub App conversion | >8% | Funnel health |

---

## 16. Engineering Hiring

**Founding (3 technical):**

1. **CTO / Systems Architect** — owns data model, parser, platform. Ideal background: ex-HashiCorp, ex-Datadog, ex-Weaveworks, ex-Kubecost, ex-CNCF maintainer. Go + Kubernetes internals required.
2. **Platform / Infra Engineer** — deep Kubernetes, Prometheus internals, in-cluster agent work. Ex-SRE at a K8s-heavy shop. client-go expertise.
3. **Agent / LLM Engineer** — Apply Fix pipeline, prompt engineering, cost controls. Strong Go + LLM orchestration + prompt security experience.

**Months 3–9 Hires:**
- Senior Backend Engineer (Month 6) — cost engine depth
- Developer Advocate (Month 9) — community, KubeCon, content
- Security Engineer (Month 11) — SOC 2 prep, red team

**At least the first 4 hires must have strong Kubernetes experience.** Non-negotiable for CNCF-community credibility and the technical depth the product requires.

---

## 17. Honest Open Questions

These are the problems we don't fully know how to solve yet. We'll resolve them in the first 6 months or the company doesn't make it.

1. **Helm templating edge cases at scale.** Bitnami-style deep-nested sub-charts, custom `_helpers.tpl`, Helmfile umbrella patterns. Plan: test against the top 50 public charts, open-source the parser if needed to get community contributions, fail loudly when parsing is incomplete.

2. **Customers without Prometheus.** Some smaller shops run only metrics-server. Plan: "limited data mode" clearly labeled; no Apply Fix, only cost-per-PR; upgrade path is "install Prometheus or use our managed Prometheus integration."

3. **Multi-environment Helm PRs.** A single PR touches a base used by dev+staging+prod overlays. Per-overlay analysis with stricter thresholds for prod changes.

4. **Service mesh presence variability.** With Istio/Linkerd, we have rich dependency data. Without, we fall back to labels + naming heuristics. Clear confidence downgrade when mesh data is missing.

5. **PR noise control.** 50 opportunities found at once shouldn't become 50 PRs. Plan: batched rollups per-repo per-day, max 3 PRs per repo per day, ranked by savings magnitude.

6. **Postgres scale ceiling.** When does a single Postgres stop working? Rough estimate: ~5K customers × 1K workloads × 90 days of metrics = ~450M rows in `workload_metrics`. Timescale handles this comfortably. Beyond that, partition by tenant or migrate the time-series to ClickHouse. Decision forced by data, not by theory.

7. **CUR data latency and completeness.** AWS CUR is 24-48h delayed. Receipt verification is 30-day windowed. For the 24-48h edge, we reconcile on every new CUR export and update receipts if the delta changes meaningfully.

8. **Cross-cluster cost attribution for multi-cluster workloads.** If `checkout-api` runs in 5 clusters, cost attribution needs cluster-level awareness. Plan: per-cluster attribution, aggregated in Receipts.

---

## 18. One-Paragraph Summary

Optiqor is a modular-monolith Go backend on AWS that reads customer Kubernetes clusters (via an open-source in-cluster agent using client-go informers + Prometheus queries) and customer GitHub repositories (via a GitHub App), merges that state into a Postgres + TimescaleDB data model with strict row-level-security multi-tenancy, runs deterministic classifiers over workloads to identify overprovisioning and security issues, orchestrates Anthropic Claude models (Haiku for enrichment, Sonnet for Helm values diff generation, Opus for escalation) through a defense-in-depth prompt pipeline with input sanitization / structured prompts / second-pass validator / deterministic post-validators, computes a rule-based Confidence band using Prometheus-grounded signals, opens pull requests with one-click Apply Fix through a signed-token flow, monitors merged changes for 7 days against statistical baselines (Box-Cox transformed cost signals, STL-decomposed latency) via Auto-Rollback Guard in a phased observe-then-act rollout, attributes AWS CUR costs to pods via a hybrid requests+usage model with published methodology, and issues Ed25519-signed Receipts verifying savings 30 days after merge. The entire stack runs on boring well-understood technology (PostgreSQL, Redis, Go, Temporal, EKS), is built by 3 founding engineers in 12 weeks, and is deliberately the opposite of the "nine microservices in three languages with a graph database" that kills most infra startups.

---

*Document version: 4.0 · Full senior-architect rewrite: boring-tech stack, modular monolith, Postgres-only (no Neo4j), Go-only (no Python), row-level security multi-tenancy, Box-Cox+STL statistics for Auto-Rollback, defense-in-depth prompt security, explicit CUR attribution methodology, no ephemeral clusters for validation · April 2026*
