# ADR-0014: Redis as cache and rate-limit substrate, never source of truth

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Data

## Context

Redis is in the Optiqor stack from day one per CLAUDE.md, alongside Postgres + TimescaleDB. It serves three operational roles:

1. **Cache** for hot read paths (tenant lookup by API token, parsed Helm chart memo, recent recommendation snapshot).
2. **Rate limit substrate** for synchronous API tier — token-bucket counters keyed by tenant + endpoint.
3. **LISTEN/NOTIFY assist** when Postgres's native pub/sub backpressures (Year 1: rare; Year 2+: maybe).

ADR-0002 made Postgres + TimescaleDB the only primary store. Redis is not a primary store — it's an accelerant. But the boundary between "cache" and "primary store" erodes silently in practice. An engineer writes a Redis-only counter for "API calls this hour." Six months later that counter is being used to drive billing. Now Redis is a primary store for a financial number, and nobody noticed.

This ADR pins the invariants that prevent that erosion.

## Decision

Redis serves only as a **cache** and a **rate-limit substrate**. It is **never the source of truth** for any data Optiqor relies on for correctness.

Concretely, the rules:

1. **Every Redis value must be reconstructable.** For any key in Redis, the system can rebuild that key's value by re-reading Postgres (or by recomputing from inputs available in Postgres / S3). If a Redis flush wipes the database, the product degrades to "slower" — never to "wrong" or "lost."

2. **No Redis-only writes.** Code that writes to Redis must also write the same fact to Postgres (or compute it from a Postgres-authoritative source). The Redis write is the cache fill; the Postgres write is the truth.

3. **No financial data in Redis.** Billing line items, Receipt payloads, Cost-Spike thresholds, llm_calls cost rows, anything that feeds an invoice or a Receipt — these go to Postgres directly. Cache reads are fine; cache writes that aren't backed by Postgres are forbidden.

4. **No Redis-keyed cross-tenant aggregation.** Cross-tenant aggregations (Leaderboard, pattern-library counts) run against Postgres with `is_superuser_context()` per ADR-0003. Redis is not the aggregation surface.

5. **Tenant-prefix every key.** Per CLAUDE.md: every Redis key is prefixed `t:<tenant_id>:`. Helpers in `platform/db/redis` enforce this. A direct `redis.Client` call outside the wrapper is a P0 bug. Cross-tenant keys (very few — system-wide config, global rate-limits) use `sys:` prefix.

6. **Cache invalidation is explicit.** TTL is the default invalidation; explicit `DEL` on write-through to Postgres is the supplement. No reliance on Redis eviction policies for correctness.

## Alternatives considered

**Alternative 1: No Redis at all; Postgres for everything.**
Tempting per the "boring single database" principle from ADR-0002. Rejected because: rate-limiting on every API call goes through Postgres, which costs a round-trip + lock contention on a hot row. At low scale Postgres handles this fine; at the 100-tenant mark, Redis is meaningfully better for this specific workload. The cost-benefit tilts toward "use Redis for what it's good at, keep Postgres for truth."

**Alternative 2: Redis as the primary store for ephemeral state.**
Make Redis authoritative for things like "current rate-limit count" or "last-seen webhook timestamp," with no Postgres backing. Rejected because: the invariant that "we can rebuild everything from Postgres + S3" is load-bearing for disaster recovery. A Redis primary store breaks DR.

**Alternative 3: Wait until ~50 tenants before introducing Redis.**
Earlier strategic guidance suggested deferring Redis introduction until demand justified it. Rejected for the codebase as it actually exists: Redis is already wired (CLAUDE.md line 19, present in `platform/db/redis`, used for rate limits + LISTEN/NOTIFY fallback). Ripping it out to "simplify" would be net-negative. The invariant worth pinning is *what Redis is allowed to do*, not *whether Redis should be present*. This ADR pins the former.

**Alternative 4: Memcached or KeyDB instead of Redis.**
Equivalent for cache + rate-limit use cases. Rejected because: Redis is already in the stack, has tenant-prefix wrappers, and the operational team knows it. Switching is a future operational decision (cost, support, managed-service availability), not an architectural one. This ADR's invariants apply regardless of which Redis-compatible engine is deployed.

## Consequences

**Easier:**
- Disaster recovery is straightforward. Postgres backup + S3 versioning is sufficient — Redis can be reflushed without data loss. We never have to "restore Redis from a backup."
- Multi-region failover is simpler. Redis in each region is a local cache; the source of truth (Postgres) is what gets replicated.
- New engineers know the rule: "If it's in Redis, it's also in Postgres." Easy to remember, easy to enforce.
- Cache poisoning has bounded impact. A bad cache value is a stale read until invalidation; it never becomes the wrong truth.

**Harder:**
- Some workloads tempt engineers toward Redis-only writes (high-volume counters, ephemeral session state). The discipline to also write to Postgres is required. **Mitigation: code review explicitly checks for Redis-write-without-Postgres-write in non-cache-fill contexts. Lint rule (eventually): Redis `SET` / `INCR` / `HSET` calls outside `platform/db/redis/cache.go` must be flagged for human review.**
- Cache-fill latency on cold reads. First read pays Postgres + Redis write cost; subsequent reads are Redis-only. This is the standard cache trade-off and is accepted.

**Locked into:**
- Postgres as the authoritative store for every fact Optiqor cares about being correct.
- The tenant-prefix invariant (`t:<tenant_id>:` keys) per CLAUDE.md.
- Operating Redis (or a Redis-compatible service like ElastiCache, managed in AWS for Year 1). The operational tax is paid.

**When we'd revisit this:**
- If a use case appears where Redis-only writes are genuinely the right design (e.g., a high-frequency telemetry counter that doesn't need DR), it gets its own ADR justifying the carve-out. This ADR's invariants are the default; exceptions are documented decisions.
- If managed-Redis pricing becomes operationally painful at scale, evaluate alternatives — but this is an operational decision, not architectural. The invariants here apply to whichever Redis-compatible service is deployed.

## Open questions

- Specific Redis-compatible service in production. Year 1: ElastiCache (AWS-managed). Year 2: re-evaluate based on cost and feature requirements.
- Whether to add a CI lint rule that fails the build on `redis.Client` calls outside the `platform/db/redis` wrapper. Lean toward yes; mechanical to implement, prevents the most common violation of Rule 5.
- Whether Postgres LISTEN/NOTIFY should ever be replaced by Redis pub/sub. Year 1: Postgres LISTEN/NOTIFY is the default per CLAUDE.md anti-pattern list (don't reach for Kafka). Year 2+: if Postgres backpressures, Redis pub/sub is the next step before Kafka — but pub/sub messages still must not be the source of truth for anything.

## Cross-references

- ADR-0002 — Postgres + TimescaleDB as the only primary store. This ADR is the corollary: Redis is allowed in the stack, but not as a primary store.
- ADR-0003 — Multi-tenancy via RLS. The tenant-prefix invariant for Redis keys mirrors the tenancy guarantees Postgres RLS provides.
- CLAUDE.md "Multi-tenancy (non-negotiable)" section — operational rules for Redis tenant prefixing.

## Implementation status

**Partial.** Redis is in the stack per CLAUDE.md and used for rate limits + LISTEN/NOTIFY fallback. Tenant-prefix wrapper at `internal/platform/db/redis` (per CLAUDE.md). **Not yet shipped:** CI lint rule that fails the build on direct `redis.Client` calls outside the wrapper; Redis-write-without-Postgres-write reviewer checklist item. These guardrails land in Phase 2+ as Redis usage broadens.

*Last verified: 2026-05-18.*
