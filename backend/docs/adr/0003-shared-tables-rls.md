# ADR-0003: Multi-tenancy via shared tables with Row-Level Security

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Data

## Context

Optiqor serves multiple customer organizations (tenants) from the same backend. The data of one tenant must never be visible to another, both for trust reasons (customers expect strict isolation) and for compliance reasons (SOC 2 and beyond require demonstrable isolation).

There are three classic approaches to SaaS multi-tenancy, each with different trade-offs:

1. **Shared tables with `tenant_id` column** — one database, one schema, every row tagged.
2. **Schema-per-tenant** — one database, separate schema per tenant.
3. **Database-per-tenant** — separate database per tenant.

The strictest enterprise customers eventually demand option 3 for residency or audit reasons, but it's operationally expensive to start there. The correct pre-seed answer is the simplest one that's also genuinely safe.

## Decision

**Shared tables with `tenant_id` on every tenant-scoped row, enforced by PostgreSQL Row-Level Security (RLS).**

Mechanism:
- Every tenant-scoped table has a `tenant_id` column with a foreign key to `tenants`.
- Every such table has RLS enabled and a policy that filters by `current_tenant_id()`.
- `current_tenant_id()` reads `app.current_tenant_id` from the current transaction's session settings.
- The application sets this via `SET LOCAL app.current_tenant_id = '<uuid>'` at the start of every request transaction.
- The application connects to Postgres as `optiqor_app`, a **non-owner role**. RLS does not apply to table owners, so the app role must be separate from the role that ran the migrations.

Trusted background jobs that legitimately operate across tenants (nightly pattern-library aggregation) set `SET LOCAL app.bypass_rls = on` at the start of their transaction. The `is_superuser_context()` function checks this; RLS policies allow access when it's true.

**Schema-per-tenant and database-per-tenant are explicitly deferred.** They become available as an enterprise-tier deployment option, not the default architecture.

## Alternatives considered

**Alternative 1: Application-level tenant filtering only (no RLS).**
Every query in the application code includes `WHERE tenant_id = ?`. Simpler. Rejected because: a single missed `WHERE` clause leaks data across tenants. RLS is the database itself enforcing the invariant — defense in depth at zero ongoing cost.

**Alternative 2: Schema-per-tenant from day one.**
Each tenant gets their own schema; queries are scoped by setting `search_path`. Rejected because: schema migrations have to be applied to N schemas instead of one; cross-tenant analytics (for the pattern library moat) require iterating across schemas instead of one query; tooling support is patchier. Worth the trade-off only when a specific enterprise contract requires it.

**Alternative 3: Database-per-tenant from day one.**
Strongest isolation. Rejected because: operationally untenable at small scale — backups, migrations, monitoring for every database. Reserved for the highest tier when a specific customer pays for it.

**Alternative 4: Multi-DB with sharding.**
Tenants assigned to one of N databases for scale. Rejected because: premature complexity. Single-database Postgres scales further than most pre-seed teams imagine; sharding is a problem for our future selves.

## Consequences

**Easier:**
- One migration runs against one database; every tenant gets the new schema instantly.
- Pattern-library aggregation is a single query (`is_superuser_context()` makes it possible).
- Backup, restore, monitoring all run against one target.
- Easier to investigate cross-tenant issues during debugging.

**Harder:**
- Every background job that operates on tenant data must explicitly set the tenant context. Forgetting it means the query returns no rows (RLS fails closed). This is safer than the opposite, but it's a foot-gun for new engineers. **Mitigation: a single middleware/wrapper that sets context at the start of every job; engineers don't write SET LOCAL themselves.**
- A bug in RLS policies or in the application's tenant-context setting could expose data. **Mitigation: integration tests that explicitly try to access tenant B's data from tenant A's context and assert that they get nothing.**
- Migrating a specific tenant to their own database later (when they pay enterprise) requires a per-tenant data extraction job. Not impossible, but real work.

**Locked into:**
- The `optiqor_app` non-owner role must exist and be the role the application connects as. If we accidentally connect as the owner, RLS is silently bypassed. Operations runbook must guard this; integration tests must verify it.
- `FORCE ROW LEVEL SECURITY` could be used to apply RLS even to the owner as an additional safety net; under consideration as a defense-in-depth measure.

**When we'd revisit this:**
- When a specific enterprise customer requires schema-per-tenant or database-per-tenant under contract. At that point, we add it as a tier-specific deployment option, not as a replacement for the default.
- When Postgres can no longer hold the combined dataset (well beyond pre-seed).

## Open questions

- Whether to use `FORCE ROW LEVEL SECURITY` from day one for additional safety. Slight performance cost; nontrivial safety win. Lean toward yes.
- How exactly the eventual database-per-tenant tier integrates with the methodology library (which assumes "ask Postgres for tenant X's data"). Design when we get there.

## Implementation status

**Shipped.** RLS policies on every tenant-scoped table in `migrations/0001_baseline.sql` and `migrations/0003_tenancy_primitives.sql`. `app.tenant_id` session variable bound via `internal/platform/db.BindTenant`. `is_superuser_context()` escape hatch is audit-logged per CLAUDE.md. `optiqor_migrator BYPASSRLS` role split is in place. `migrations/migrations_test.go` pins the structural invariants.

*Last verified: 2026-05-18.*
