# ADR-0002: Postgres + TimescaleDB as the only primary store

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Data

## Context

Optiqor's data has two distinct shapes:

- **Relational/structured** — tenants, clusters, workloads, recommendations, receipts. Low volume, frequently joined, frequently updated, high value per row.
- **Time-series** — metric samples (CPU/memory per pod every 5 minutes), billing line items (per resource per hour). Massive volume, write-once, never updated, queried by time range.

The naive instinct is to pick a specialized store for each: Postgres for the relational side, InfluxDB / ClickHouse / VictoriaMetrics for time-series, possibly Redis for caching, possibly S3-backed Parquet for long-term storage. Each additional store is operational burden: backups, monitoring, schema migrations, failover, an additional 3am page.

Operational simplicity matters more than perfect specialization at pre-seed scale.

## Decision

**One primary database: PostgreSQL with the TimescaleDB extension.** Time-series data lives in TimescaleDB hypertables, relational data in normal tables, same database, same connection, same operational story.

**One blob store: S3** (or cloud equivalent) for things that don't belong in a database: raw CUR Parquet files, signed receipt artifacts, transparency log entries.

**No other primary storage.** Specifically:
- No separate time-series database (TimescaleDB handles it).
- No MongoDB or document store (Postgres JSONB handles the few document use cases).
- No Redis as primary storage. Redis (or in-process LRU) may be introduced later as a *cache*, never as a source of truth.
- No Elasticsearch (Postgres full-text or external service when needed).

## Alternatives considered

**Alternative 1: Postgres + dedicated TSDB (InfluxDB, ClickHouse, VictoriaMetrics).**
The "best tool for each job" school. Rejected because: TimescaleDB hypertables genuinely handle metric-sample scale (billions of rows) with native Postgres tooling — backups, replication, query language. Operating two databases doubles the surgical risk and the on-call burden. The marginal performance gain from a specialized TSDB doesn't justify the cost at our scale.

**Alternative 2: ClickHouse as primary for analytics.**
Tempting for the eventual analytics workload. Rejected for now because: ClickHouse is excellent at OLAP scan-heavy queries but worse at the OLTP write-heavy patterns that dominate Optiqor's workload (ingesting metrics, opening PRs, recording state changes). TimescaleDB's hybrid model fits Optiqor's hybrid workload.

**Alternative 3: DynamoDB or other managed NoSQL.**
The "fully managed, infinitely scalable" school. Rejected because: Optiqor's data is deeply relational (workloads belong to clusters belong to tenants; receipts reference recommendations reference workloads). Forcing this into NoSQL creates either expensive joins-in-application-code or denormalization bugs. Postgres is the right shape.

**Alternative 4: Snowflake / BigQuery for the warehouse layer.**
Eventually we will want a warehouse for cross-customer analytics (the pattern library). Not Year 1. The right time is when the volume genuinely outgrows TimescaleDB's analytical capability, which is well beyond pre-seed scale.

## Consequences

**Easier:**
- One backup strategy, one replication strategy, one monitoring dashboard.
- Engineers learn one query language (SQL), one client library.
- Cross-table queries (joining workloads to recommendations to receipts) are native, not RPCs.
- Single transaction can span relational and time-series data when needed.

**Harder:**
- TimescaleDB is a Postgres extension; some managed Postgres providers don't support it. We must choose providers that do (Timescale Cloud, AWS RDS with extension, self-hosted). This constrains hosting options.
- Time-series-specific performance optimizations (column-store, specialized indexes) require TimescaleDB feature knowledge that not every engineer has. Operational documentation needed.
- Long-term metric retention will eventually pressure even TimescaleDB; we'll need to downsample to lower granularity over time. Retention policies handle this for Year 1-2.

**Locked into:**
- TimescaleDB as the extension. If TimescaleDB licensing or maintenance changes in a way that's a problem, we'd need to migrate hypertables to something else. Mitigation: hypertables degrade gracefully — if TimescaleDB became unavailable, the data is still in Postgres; we'd lose continuous aggregates and compression but the schema would still function.

**When we'd revisit this:**
- If the metric-sample ingest rate exceeds what a single Postgres write-master can handle (estimated >100k samples/second sustained), we'd need to introduce a specialized TSDB.
- If we onboard a customer with a workload that genuinely requires another data store (e.g., a graph database for some relationship analysis), we'd add it for that workload only — never as a general second store.

## Open questions

- Single multi-tenant database vs database-per-tenant for enterprise customers: deferred to ADR-0003 (multi-tenancy).
- Backup retention policy specifics: operational concern, not architectural.

## Implementation status

**Shipped.** `internal/platform/db/` is the sole primary-store wrapper. TimescaleDB extension provisioned in `migrations/0001_baseline.sql`. No other primary store imported in `go.mod` (Redis is a cache + rate-limit substrate per ADR-0014, not a primary store).

*Last verified: 2026-05-18.*
