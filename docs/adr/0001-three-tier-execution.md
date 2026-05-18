# ADR-0001: Three-tier execution architecture

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Top-level

## Context

Optiqor's workload spans three fundamentally different time scales:

1. **Real-time (seconds):** PR webhook arrives → analyze diff → post comment. Customer's developer is waiting; latency directly damages product experience.
2. **Near-real-time (minutes):** Agent reports metric samples; ingest, aggregate, run continuous aggregates. High volume, eventual consistency acceptable.
3. **Batch (hours to days):** Daily CUR ingestion, 30-day receipt signing, cross-customer pattern library aggregation. Slow, durable, multi-step workflows.

These three time scales have different reliability requirements, different scaling profiles, and conflicting failure modes. A slow CUR job at 3am cannot block PR analysis at 9am. A burst of agent metrics cannot starve receipt signing. A signing failure cannot lose data.

The common pre-seed mistake is to put "do the work" inside the request handler that triggered it. This works for demos and breaks at the first real customer.

## Decision

Optiqor's backend separates into **three execution tiers**, all running the same binary in different modes against the same Postgres database, with Temporal as the workflow orchestrator:

1. **Synchronous API tier** (`cmd/api`) — handles webhooks, dashboard requests, CLI calls. Strict latency SLO (p95 < 500ms). Stateless. Horizontally scalable. Signals Temporal workflows; never executes work inline.
2. **Async worker tier** (`cmd/worker`) — runs Temporal worker pool against per-tenant task queues. Durable, retryable, observable. This is where math runs, where validation gates fire, where PRs get opened.
3. **Background data pipeline tier** — ingests metric samples and CUR data via Temporal cron + activity workflows. High throughput, eventual consistency.

The API tier **never does work that takes longer than a single HTTP request**. It validates, signals a workflow (or starts one), returns. The worker picks up the workflow and does the actual computation. Multi-day workflows (Receipt verification at +30d, Auto-Rollback watchdog over 7d) ride Temporal's durable timers, not application-level cron.

## Alternatives considered

**Alternative 1: Single-tier monolith doing everything inline.**
Simpler to start. Rejected because: PR webhooks need <2 second response or GitHub starts treating Optiqor as unreliable; cost attribution math takes 30+ seconds per workload; these are incompatible in one tier.

**Alternative 2: True microservices from day one.**
Each tier as its own service with its own deployment, its own datastore, its own API. Rejected because: microservice complexity at pre-seed is operationally devastating; you spend more time on RPC plumbing than product. The right time for true microservices is when team size forces it (~Year 3).

**Alternative 3: Function-as-a-service (Lambda/Cloud Run) for everything.**
Tempting for the "no infrastructure" pitch. Rejected because: cold starts hurt webhook latency; long-running workflows (30-day signing) don't fit; observability across many functions is harder than across one binary.

## Consequences

**Easier:**
- API tier scales independently of worker tier; webhook spikes don't slow background work.
- Workers can be killed and restarted freely; jobs resume from the queue.
- Single binary means single test suite, single deploy pipeline, single observability story.

**Harder:**
- Engineers must remember: "if it takes longer than a request, it goes in a worker." This requires discipline.
- Local development must run all three tiers (a `make dev` command that runs api + worker + pipeline against a local Postgres).

**Locked into:**
- **Temporal as the workflow orchestrator from day one.** Per-tenant task queues (`tenant-<uuid>-default`, `tenant-<uuid>-priority`) are the isolation primitive; CLAUDE.md enforces this. Temporal workflow signatures in `internal/worker/workflows/` are a public surface — breaking changes require a deprecation window. We pay the operational complexity of running Temporal (cluster, history service, matching service, visibility store) because Year 1 already has multi-day workflows: 30-day Receipt verification, 7-day Auto-Rollback watchdog, cost-spike correlation windows. A Postgres-backed queue (river / asynq) was considered and rejected; the migration path from a simple queue to Temporal is straightforward but lossy (in-flight jobs at the cutover), and starting on the durable-workflow primitive avoids a forced migration mid-Year-1. The complexity tax is paid; we don't claw it back to "simplify."

**When we'd revisit this:**
- If we cross ~50 engineers and the monolith becomes a bottleneck for team coordination, we split into microservices then.
- If we add a synchronous use case that genuinely needs Lambda-style elasticity (unlikely at our profile).
- If Temporal's operational cost becomes disproportionate to throughput before Year 2, evaluate Temporal Cloud as a managed option. Code changes are zero; only deployment changes. This is a deferred operational decision, not an architectural one.

## Open questions

- Whether the synchronous tier should be replicated across regions for latency. Year 2 problem.
- Self-hosted Temporal cluster vs Temporal Cloud. Operational, not architectural; defer until cost/throughput data justifies the trade-off.

## Implementation status

**Shipped.** Three binaries produced from one repo: `cmd/api`, `cmd/worker`, `cmd/agent`. Temporal SDK pinned in `go.mod` (`go.temporal.io/sdk v1.43.0`). Workflow signatures live in `internal/worker/workflows/`. The agent's watch loop is itself a Phase-5 stub (`cmd/agent/main.go` idles on SIGTERM); the three-tier separation is in place, the agent's actual data collection is not.

*Last verified: 2026-05-18.*
