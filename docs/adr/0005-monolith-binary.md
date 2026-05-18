# ADR-0005: Monolith binary with mode flags, not microservices

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Backend

## Context

ADR-0001 established three execution tiers: synchronous API, async workers, background pipeline. The natural follow-up question is: are these three tiers three separate codebases (microservices) or one codebase running in different modes?

The pre-seed industry default for the last decade has been "microservices first." This is usually wrong. Microservices solve a team-coordination problem (many teams, many languages, independent deployment). At pre-seed, with one to five engineers, the team-coordination problem does not exist. What microservices give you instead is RPC complexity, distributed system failure modes, observability fragmentation, and slower development.

But the three tiers genuinely have different scaling profiles. The API tier needs to scale with webhook volume; workers scale with job depth; the pipeline scales with metric ingest rate. We need *operational* independence.

## Decision

**One Go binary, one repository, deployed as multiple Kubernetes workloads.** The binary accepts a `--mode` flag (or `OPTIQOR_MODE` env var) that selects which entrypoint runs:

- `--mode=api` — HTTP server for webhooks, dashboard, CLI calls
- `--mode=worker` — pulls from the job queue, runs the math
- `--mode=signer` — receipt signing service (isolated for security; see ADR-0011)
- `--mode=ingest` — agent metric and CUR data ingestion

Same Go module, same dependencies, same test suite, same observability. Different mode flags get deployed as different Kubernetes Deployments, scaled independently.

The signer mode is treated specially: it runs in a more locked-down environment (more restrictive IAM, no internet access except to KMS), but it's still the same binary. This makes the signer's behavior auditable as part of the main codebase rather than a separate repo with separate review.

## Alternatives considered

**Alternative 1: True microservices from day one.**
Separate repos, separate deploy pipelines, RPC between services. Rejected because: at pre-seed team size, you spend more time on RPC plumbing than on product. The Stripe/Shopify/Datadog playbook starts with a monolith and splits when team size demands it.

**Alternative 2: Single binary, single deployment, all modes always active.**
Simplest. Rejected because: we'd lose the ability to scale tiers independently. A burst of webhook traffic could starve workers of CPU on the same pod.

**Alternative 3: Modular monolith with internal "service" boundaries.**
The compromise approach: separate packages with clean APIs, deployed as one binary, can later be split. **This is what we're actually doing inside the monolith.** ADR-0005 is just about the deployment topology; internal organization is in ADR-0006.

**Alternative 4: Serverless (Lambda, Cloud Run) for everything.**
Tempting for the "no ops" pitch. Rejected because: cold starts hurt webhook latency; long-running workflows (receipt signing over days) don't fit; observability across many functions is harder than across one binary; vendor lock-in is severe.

## Consequences

**Easier:**
- One repository, one CI pipeline, one test suite. New engineers onboard in days, not weeks.
- Refactors that span tiers (the API and the worker both need to understand a new data type) are atomic — change once, all callers updated.
- Local development: a single `make dev` command runs all modes against a local Postgres. Engineers can debug end-to-end flows on their laptop.
- Same observability stack across all tiers; one place to look for problems.

**Harder:**
- The binary is bigger than any single microservice would be. Deployment image is larger; cold start (on K8s pod creation) is slower. **Mitigation: keep dependencies lean; Go's static linking helps.**
- A bug in one mode (say, the signer) could in principle cause an incident that affects the same binary running as API. **Mitigation: integration tests cover mode isolation; the signer's logic should not depend on API code paths or vice versa.**
- It's tempting to share too much code between modes. **Mitigation: enforce that each mode has a clear `cmd/<mode>/main.go` entrypoint, and mode-specific code lives in `internal/<mode>/`. Cross-mode shared code lives in `internal/` packages with no mode prefix and is explicitly reviewed.**

**Locked into:**
- Go as the backend language. Switching languages would require rewriting the entire monolith. Acceptable lock-in — Go is the right language for this workload (concurrent network and database work, deploy as a static binary, good ecosystem for K8s integration).
- Single repository structure. We could split later, but the monorepo is the source of truth until proven inadequate.

**When we'd revisit this:**
- When team size exceeds ~15 engineers and merge conflicts in the monorepo become a daily problem.
- When a specific mode genuinely needs a different language (e.g., heavy ML in Year 2-3 might justify a Python service for the model serving layer — see ADR-0007's open question).
- When deploy speed of the full binary becomes a bottleneck (with proper CI caching, this is far away).

## Open questions

- Repository structure detail (where exactly does shared code go, how are internal package boundaries enforced). Implementation detail; document in `CONTRIBUTING.md`, not in an ADR.
- Whether the eventual self-hosted on-prem edition is also one binary with a different mode, or a fundamentally different deployment. Lean toward one binary; defer to when the on-prem edition is real.

## Implementation status

**Shipped.** Three entry points: `cmd/api/main.go`, `cmd/worker/main.go`, `cmd/agent/main.go`. Shared code in `internal/` and `internal/platform/`. `Dockerfile` builds one binary image. `docker-compose.yml` runs all three modes locally. The signer mode (`--mode=signer` per ADR-0011) is a planned addition; the binary-shape decision is shipped.

*Last verified: 2026-05-18.*
