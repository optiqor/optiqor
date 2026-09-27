# 19. Service graph stored as a TimescaleDB hypertable, re-materialised per snapshot

Date: 2026-05-29
Status: Accepted

## Context

PR #32 introduced the workload→service→endpoint adjacency the
validator pipeline uses to detect "no live traffic" workloads (safe to
scale to zero) and to refuse Apply Fix recommendations that would
break dependents.

The agent ships the entire graph (typically 10-200 edges) in every
snapshot. The backend needs to:

1. Answer "what services select this workload?" in < 100 ms for the
   validator pipeline gate.
2. Retain at least a 7-day window so we can detect "no traffic for
   the last 7 days" reliably.
3. Stay cheap to write — every agent snapshot writes the full graph,
   which is wasteful but simplifies the agent (no delta computation).

## Decision

Backend table `service_graph_snapshots`:

- TimescaleDB hypertable keyed by `(tenant_id, captured_at)`.
- One row per `(workload, service, endpoint)` triple per snapshot —
  the full graph re-materialised, not a delta.
- 7-day retention policy via `add_retention_policy`.
- Continuous aggregate `service_graph_daily` for the validator's hot
  query (most-recent per (workload, service)).
- RLS-bound; tenant_id in every row.

The validator's hot query reads from the continuous aggregate, not
the raw hypertable, so the read path stays sub-100 ms even at the
~50k tenant scale.

## Why re-materialise, not diff?

- Agent code stays trivial: walk informers, emit, ship. No
  diff-and-merge state held in the agent.
- Diffs need a reliable "last seen" reference. After a snapshot fails
  to POST, the agent and backend disagree about what was last
  acknowledged; reconciliation is complex and rare-path-tested badly.
- 200 edges × per-row JSON overhead × 1440 snapshots/day is ~30 MB
  per cluster per day. With 7-day retention that's 210 MB per cluster
  — small enough not to matter at our envelope.
- Phase 6's CUR ingestion is the data-volume scaling concern; service
  graph isn't on the long pole.

## Why TimescaleDB?

- The 7-day retention plus the continuous aggregate gives us the
  "last seen" semantics for free without a custom GC.
- The Optiqor stack already uses Timescale for `workload_observed_state`
  (migration 0002); no new database to operate.
- `chunk_time_interval => '1 day'` produces 7 chunks total — well
  within Timescale's recommended envelope.

## Consequences

- Validator caches the hot query for 30s per (workload, service);
  cached miss falls through to Timescale. Tested at the
  applyfix/validator/ pipeline level.
- Phase 7's cross-cluster workload classifier reads the same hypertable
  through `is_superuser_context()`; the RLS predicate accepts the
  bypass and audit_log captures the flip.
- Backfill from a missed snapshot is intentionally not supported —
  the gap shows up as missing rows for that 60-second window and
  the validator falls back to "no graph signal" rather than guessing.

## Alternatives considered

- **Neo4j**: dropped in Phase 1 (ADR-0002). Recursive CTEs cover our
  graph needs; a graph database is over-engineering for a flat
  adjacency list.
- **JSONB column on `workloads`**: row size grows unbounded; killing
  the row-level cache. Hypertable shape works.
- **Materialised view re-built on snapshot**: refresh latency would
  bottleneck Apply Fix dispatch; the continuous aggregate's lazy
  refresh is the right shape.
