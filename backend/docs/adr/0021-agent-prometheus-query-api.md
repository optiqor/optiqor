# ADR-0021: Agent reads customer Prometheus via Query API; exposes its own `/metrics`

**Status:** Accepted
**Date:** 2026-05-29
**Domain:** Agent

## Context

The agent has two distinct observability needs:

1. **Read customer workload metrics** (CPU/memory percentiles over 30 days, OOMKilled events, HPA spec, requests/limits, restarts). These drive the sizing math and the Validator pipeline.
2. **Expose the agent's own internal metrics** (query latency, query errors, snapshot ship duration, informer cache size). These are how the customer's on-call team monitors the agent.

A naive build conflates them: scrape every pod's `/metrics` directly, run a parallel collector in the cluster, and emit aggregates upstream. That doubles the network and CPU cost of monitoring, gives different numbers than the customer sees in their own Grafana (credibility disaster), and breaks the trust property — the customer's engineer must be able to verify a Receipt's "P99 memory was 380MB" claim by running the same PromQL in their Grafana and getting the same number.

## Decision

Split the two needs cleanly. The agent reads customer workload data **only** via the customer's Prometheus HTTP Query API (`/api/v1/query`, `/api/v1/query_range`). The agent exposes its **own** metrics via a `/metrics` endpoint that the customer's Prometheus scrapes.

- **Read path:** `internal/agent/prom.Client` interface with `Query` + `QueryRange` methods; `HTTPClient` is the production implementation. PromQL is centralised in `internal/agent/prom/canonical.go` so every query carries the `container!="POD", container!=""` filter and uses `container_memory_working_set_bytes` (never `usage_bytes`) by construction.
- **Write path:** the agent's `/metrics` endpoint at `:8088/metrics` exposes the existing `internal/platform/telemetry.Registry` content. Customer Prometheus scrapes it via a ServiceMonitor or PodMonitor; the Helm chart ships the manifests.

The agent never runs its own scraper against application pods, never installs its own collectors, never duplicates kube-state-metrics or cAdvisor.

## Alternatives considered

**Push-based via OpenTelemetry export.** Agent could push samples upstream via OTLP. Rejected because the customer's existing Prometheus already has 30 days of history we'd need to rebuild; pushing means we lose backward access to that history and can't verify our own numbers against the customer's source of truth.

**Bundle our own Prometheus inside the agent.** Considered for clusters without an existing TSDB. Rejected because every customer running production K8s has Prometheus (or a Prometheus-compatible store: VictoriaMetrics, Thanos, Mimir, Grafana Cloud). If they don't, they're not Optiqor's ICP. Bundling adds a multi-GB binary, a storage backend, and a maintenance surface for zero gain.

**Direct `/metrics` scraping of customer pods.** This is the wrong default — see Context. We still need it for **one** case: the agent's own metrics emitter. That's the `/metrics` endpoint above, and it's scraped *by* the customer, not *from* the customer.

## Consequences

**Easier:**
- The signed-savings property holds without further work. The customer's engineer runs the same PromQL we ran and sees the same number.
- Auth surface is one client, one well-defined seam. `internal/agent/prom/auth.go` supports bearer / basic / SA-token (file-watched, kubelet-rotation-aware) / mTLS.
- 30-day percentile queries (`quantile_over_time(0.95, ...[30d:1h])`) run on the customer's Prometheus, which already has the data and the query engine optimised for it. Our agent is small.

**Harder:**
- We need an auth implementation for each customer's Prometheus posture. Production Prometheus is rarely unauthenticated. The `Authenticator` interface covers the four common shapes.
- 30-day range queries are expensive. The `CachingClient` enforces "run once daily during quiet hours" with a 24h TTL on range queries and a 60s TTL on instant queries. Concurrent in-flight queries collapse via singleflight so a snapshot tick firing 12 queries in parallel still costs one round-trip per unique PromQL.
- The agent depends on the customer's Prometheus being up. The scrape loop degrades gracefully: empty result on transport error, `optiqor_agent_prom_errors_total` increments, the snapshot ships with whatever K8s state we did read.

**Locked in:**
- The agent never accumulates its own metric history. If the customer's Prometheus is wiped, our recommendations regress to "no observation" instead of falling back to a cached view.

## Open questions

- Federation / remote-read. Customers running Thanos or Cortex front-ends to multiple Prometheus instances may want the agent to query a federation endpoint. The current `OPTIQOR_PROMETHEUS_URL` is a single string; multi-endpoint config lands when a partner asks.
- Range-query offload to TimescaleDB. If the customer's Prometheus can't handle 30d range queries on a 1000-pod cluster, the agent could shift to remote-read against a separate TSDB. Defer until we measure the actual cost on partner clusters.
