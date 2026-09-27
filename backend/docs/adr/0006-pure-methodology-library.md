# ADR-0006: Methodology as a pure-function library

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Backend

## Context

Optiqor's value proposition depends on a methodology — `hybrid_v1` — for cost attribution, statistical sizing, anomaly detection, and savings reconciliation. This methodology gets signed into every receipt. Customers and auditors can verify receipts independently by re-running the methodology against the same input data and confirming they get the same output.

For this trust property to hold, the methodology must be:

- **Deterministic** — same inputs always produce identical outputs.
- **Testable** — covered by an extensive test suite with fixture data.
- **Auditable** — readable end-to-end without wading through HTTP handlers, database calls, or queue logic.
- **Versionable** — `hybrid_v1` exists forever; `hybrid_v2` (when we publish it) coexists with v1 for the lifetime of v1's receipts.
- **Portable** — usable in the backend, in the CLI (subset), in customer verification tools, and eventually in an on-prem build.

The single biggest architectural mistake here would be to entangle the math with infrastructure code. If "compute the recommendation" is wired into the API handler that triggered it, the math is untestable in isolation, harder to audit, and impossible to port.

## Decision

The methodology lives in a **pure-function library** at `internal/methodology/` in the monorepo, with strict rules:

1. **No I/O.** No HTTP, no database, no file system, no logging beyond pure structured records that the caller may or may not emit.
2. **All inputs explicit.** Every function takes the data it needs as arguments; nothing comes from globals, ambient context, or hidden state.
3. **All outputs structured.** Functions return Go structs with concrete types. No string-typed return values, no opaque maps.
4. **Determinism enforced.** No random number generation (or, if required, the RNG seed is an explicit input). No clock reads inside math (the `Clock` interface from CLAUDE.md is injected — never `time.Now()` directly).
5. **Versioned subpackages.** `internal/methodology/hybrid/v1/` is the current version. When `hybrid_v2` ships, it lives at `internal/methodology/hybrid/v2/`; v1 stays in the codebase, untouched, for the lifetime of v1 receipts.

The orchestration layer (the worker tier from ADR-0001) is what fetches data, calls the methodology library, and persists results. The methodology library never knows about the database, the queue, or the customer.

**Path choice — `internal/` not `pkg/`.** CLAUDE.md is explicit: *"This repo has no public Go API surface. Everything is `internal/`."* The methodology is written *as if* it could be extracted to a separate published module (no `internal/` cross-imports inward, no dependency on `database/sql` or `net/http`), but it lives at `internal/methodology/` today. When customer-side verification tooling is ready to ship (Year 2+), we extract `internal/methodology/` into a separate Apache-2.0 repo and publish it as a standalone Go module. Until then, it stays internal — which honors the no-public-API rule and avoids the versioning/deprecation burden of a public `pkg/` surface at pre-seed.

The CLI's `pkg/rules` already re-implements a subset of the methodology for sandbox-grade analysis (±40% accuracy). The CLI does **not** import `internal/methodology/` — that would break the optiqor-cli repo's independent auditability (Apache-2.0 OSS, no proprietary dependencies). Parity is maintained by the existing `tests/integration/cli_parity_test.go` which asserts the CLI and backend produce identical `Finding` sets on canonical fixtures.

## Alternatives considered

**Alternative 1: Methodology embedded in service code.**
Math lives inside the API handlers and workers, alongside database calls. Rejected because: untestable in isolation; auditors would have to read every code path that touches a calculation; refactors risk silently changing the methodology.

**Alternative 2: Methodology as a separate microservice.**
The math runs as its own RPC service called by the orchestrator. Rejected because: adds network latency and failure modes for no benefit. The methodology has no state of its own; an RPC boundary around pure functions is pure overhead.

**Alternative 3: Methodology as a published Go module (separate repo).**
Could be useful for the on-prem edition and for customer verification tools. Rejected for *now* because: pre-seed, splitting repos prematurely creates versioning headaches. The methodology lives at `internal/methodology/` inside the main repo, but is written *as if* it were a separate module — no dependencies on infrastructure code, no imports from elsewhere in the monorepo. When we want to publish it separately, we can do so with minimal extraction.

**Alternative 4: Methodology at `pkg/methodology/` (public Go API surface).**
Earlier drafts of this ADR specified `pkg/methodology/`. Rejected because: CLAUDE.md is explicit that the repo has no public Go API surface, everything is `internal/`. Promoting methodology to `pkg/` would create versioning, deprecation, and compatibility obligations we do not want at pre-seed. The "extract-and-publish" path stays open via a future repo split; the path doesn't require it to live in `pkg/` today.

**Alternative 4: Methodology in a non-Go language (Python for ML, R for statistics).**
Tempting for the eventual ML layer. Rejected for the core methodology because: methodology runs in the request path, must be fast, must integrate cleanly with the rest of the backend. Go is fine for statistics (gonum is mature). When/if heavy ML appears, it's an opt-in subsystem (probably in Python) that the methodology library calls into as a special case — not the default.

## Consequences

**Easier:**
- The methodology can be unit-tested exhaustively against fixture data. CI runs thousands of test cases against the math without touching infrastructure.
- Customer-side verification tooling (when shipped in Year 2+) imports the eventually-extracted module and re-runs any receipt's calculation to confirm Optiqor's claim.
- The methodology spec page on `optiqor.dev/methodology/hybrid-v1` directly corresponds to the code. If they ever drift, that's a bug.
- The CLI's `pkg/rules` runs against the same canonical fixtures via `tests/integration/cli_parity_test.go`, so customers can be confident the sandbox CLI and backend agree on the same inputs (within the CLI's ±40% sandbox-grade caveat).

**Harder:**
- The orchestration layer has to do the I/O dance: fetch data, transform to methodology-input structs, call the methodology, persist results. This is more code than just inlining the math, but the I/O code is *also* testable in isolation.
- Engineers must resist the temptation to "just add a quick database call" inside a methodology function when they're refactoring. **Mitigation: lint rule that the `internal/methodology/` subtree imports nothing from other `internal/` packages and nothing from `database/sql` or `net/http`.** Make it a CI failure. The lint enforces the "extractable as a standalone module" invariant.
- Versioning forever means we accumulate dead code (v1 lingers even when v3 is the current version). Acceptable cost.

**Locked into:**
- The version-coexistence rule. Once a `hybrid_vN` is in production and has signed receipts, the code for `vN` stays in the repository forever. Receipts signed under v1 must remain verifiable in 2030. **This is non-negotiable for the trust property.**
- The methodology spec must be public. ADR-0006 implies that customers can read and verify the methodology; that requires publishing it on optiqor.dev. **The methodology page is now load-bearing infrastructure, not marketing.**

**When we'd revisit this:**
- If we discover a class of methodology computation that genuinely cannot be expressed without I/O (e.g., needs to fetch live cloud pricing during calculation). Plausible answer: pre-fetch the data into the methodology's input, keep the function pure.
- **Year 2+ extraction to a separate Apache-2.0 repo for customer-side verification tools.** The trigger is product demand (enterprise tier asking for an offline verifier, or a regulator asking for an independently-installable methodology check). The extraction is mechanical: copy `internal/methodology/` to a new repo, change the import path, publish. Backend imports the new module; behavior is unchanged. **The code shape doesn't change at extraction time — only its packaging.** That's the invariant the `internal/methodology/` lint rule preserves.

## Open questions

- Exactly what subset of methodology functions the CLI uses, vs the backend. Implementation detail; documented in the CLI's README and the methodology spec.
- Whether to release reference implementations of the methodology in other languages (Python, JavaScript) so customers can verify in their language of choice. Year 2+ question.

## Implementation status

**Partial.** Sandbox-grade rule-based engine lives at `internal/cost/pricer.go` + `internal/cost/static_pricer.go` with the `Pricer` interface seam. The full `hybrid_v1` math (CUR allocation, statistical sizing, lognormal memory, idle-capacity partitioning) is unwritten and scheduled for Phase 6 per `optiqor/todo.md`. The target package path is `internal/methodology/hybrid/v1/`; the lint guard preventing `internal/methodology/` from importing other `internal/` packages is not yet wired (lands with the first methodology PR).

*Last verified: 2026-05-18.*
