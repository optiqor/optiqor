# Optiqor — Architecture Decision Records

This directory documents the foundational architectural decisions for Optiqor.
Each ADR captures one decision: what was chosen, why, what alternatives were
considered, and what the consequences are.

ADRs are **immutable once accepted**. When a decision changes, write a new ADR
that supersedes the old one. The old ADR stays in the repo with its status
updated to `Superseded by ADR-NNN`.

## Format

Every ADR follows the same structure:

- **Status** — Proposed / Accepted / Superseded
- **Context** — what problem we're solving and why now
- **Decision** — the choice, stated plainly
- **Alternatives** — what else we considered and why we rejected them
- **Consequences** — what becomes easier, what becomes harder, what we're locked into
- **Open questions** — things we don't yet know

## Index

| # | Title | Status | Domain |
|---|---|---|---|
| 0001 | [Three-tier execution architecture](0001-three-tier-execution.md) | Accepted | Top-level |
| 0002 | [Postgres + TimescaleDB as the only primary store](0002-postgres-only.md) | Accepted | Data |
| 0003 | [Multi-tenancy via shared tables with RLS](0003-shared-tables-rls.md) | Accepted | Data |
| 0004 | [Workload identity hashing](0004-workload-identity-hash.md) | Accepted | Data |
| 0005 | [Monolith binary with mode flags, not microservices](0005-monolith-binary.md) | Accepted | Backend |
| 0006 | [Methodology as a pure-function library](0006-pure-methodology-library.md) | Accepted | Backend |
| 0007 | [LLM isolated to prose generation, never decisions](0007-llm-isolation.md) | Accepted | Backend |
| 0008 | [Agent permissions: read-only K8s, outbound-only network](0008-agent-permissions.md) | Accepted | Agent |
| 0009 | [Three-mode trust spectrum, all changes through Git](0009-three-mode-trust-spectrum.md) | Accepted | Product |
| 0010 | [Pre-merge validation gate, four stages](0010-pre-merge-validation-gate.md) | Accepted | Product |
| 0011 | [Receipt signing via KMS, never in-process keys](0011-kms-signing.md) | Accepted | Security |
| 0012 | [Coexist with VPA/HPA/Karpenter, do not replace](0012-coexist-with-primitives.md) | Accepted | Product |
| 0013 | [Engineering reference reading hygiene for ecosystem projects](0013-reference-reading-hygiene.md) | Accepted | Backend |
| 0014 | [Redis as cache and rate-limit substrate, never source of truth](0014-redis-cache-only.md) | Accepted | Data |
| 0015 | [Three-doc roadmap split (org / optiqor-roadmap / optiqor-todo)](0015-roadmap-todo-three-doc-split.md) | Accepted | Top-level |

## Reading order

For new engineers joining: read in order 0001 → 0014. Each ADR builds on the
ones before it. Reading all 14 architectural ADRs takes about 100 minutes and
gives you the full architectural model. (ADR-0015 is a documentation-process
decision, not an architecture decision; read it when you need to update the
roadmap docs.)

For specific questions:

- *Why this database?* → 0002
- *Why is the agent so locked down?* → 0008
- *Why do we sign receipts this way?* → 0011
- *What's the methodology library?* → 0006
- *Why don't we replace VPA?* → 0012
- *Can I import OpenCost / VPA / Karpenter source?* → 0013
- *What can I put in Redis?* → 0014

## How to propose a new ADR

1. Copy the template from `_template.md`
2. Number it sequentially (next is 0016)
3. Write it with status `Proposed`
4. Open a PR for review
5. Merge with status changed to `Accepted` once consensus reached

## What goes in an ADR vs. what doesn't

**Goes in an ADR:**
- Decisions that are expensive to reverse later
- Decisions where there were real alternatives
- Decisions that future engineers will need to understand the reason for

**Does NOT go in an ADR:**
- Tactical implementation choices (which Go library to use for HTTP)
- Decisions that can be trivially changed (cache size, retry count, timeout values)
- Project management decisions (sprint planning, hiring process)

When in doubt: if you find yourself explaining the same decision repeatedly,
write an ADR.
