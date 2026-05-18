# ADR-0013: Engineering reference reading hygiene for ecosystem projects

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Backend

## Context

Optiqor's value depends on its own methodology being demonstrably its own. The `hybrid_v1` cost attribution math, the statistical sizing engine, the rollback anomaly detector, the validation gate — all of these must be Optiqor's own work, signed in Receipts, defensible under technical due diligence.

But these are also problems that have already been solved publicly. The ecosystem has multiple Apache-2.0 reference implementations and CNCF-incubated projects whose codebases survived the edge cases Optiqor is about to discover:

- **OpenCost** — CUR-to-pod allocation, idle capacity partitioning, Spot/SP/RI amortization
- **Kubernetes VPA** — histogram-based sizing recommender, OOMKilled handling
- **Karpenter** — node lifetime tracking, consolidation patterns
- **kube-linter** — workload misconfiguration detection
- **kubeconform** — K8s schema validation

Pretending these don't exist is wasteful — they encode years of edge-case engineering that Optiqor would otherwise rediscover the hard way. Reading them privately as engineering reference material is legitimate, legal, and standard engineering practice (the same way one reads Postgres source to understand MVCC before building a database).

But there's a real risk: structural similarity. If Optiqor's `internal/methodology/` ends up with the same struct names, function signatures, and control flow as OpenCost's `pkg/costmodel`, or if the sizing engine reads as a VPA fork, the "we own our methodology" claim collapses. Apache 2.0 permits the copying legally; due-diligence reviewers and competitors don't care about the license — they care about whether Optiqor's product is meaningfully its own.

This ADR codifies a single uniform policy across every open-source ecosystem project an Optiqor engineer might read. One policy applied uniformly is easier to remember and easier to enforce than per-project carve-outs.

## Decision

All open-source ecosystem projects in the K8s cost / sizing / scheduling / validation space are treated as **private engineering reference material only**. The six rules below apply uniformly to OpenCost, the Kubernetes VPA recommender, Karpenter, kube-linter, kubeconform, Goldilocks, Sigstore, and any future project Optiqor learns from.

1. **Private reference only.** Engineers may read the source code privately to understand the math, edge cases, and design choices. Reading is unrestricted; the output of that reading must be Optiqor's own implementation.

2. **No `go.mod` import.** None of these projects appear in `go.mod` as direct dependencies of the methodology or its callers. A CI grep guard runs against the prohibited-imports list in `.github/lint/prohibited-imports.txt`: `github.com/opencost/opencost`, `k8s.io/autoscaler/vertical-pod-autoscaler`, and equivalents are forbidden imports. Accidental reintroduction fails CI.

3. **No surface mention.** Receipts, the methodology page (`optiqor.dev/methodology/hybrid-v1`), marketing copy, pitch decks, dashboard tiles, PR comments, sales emails, and customer-facing documentation **never** name these projects as Optiqor's sources, baselines, or inspirations. The customer surface is Optiqor end-to-end.

4. **Reviewer responsibility.** Code reviewers on methodology and detector PRs explicitly check for structural similarity to ecosystem source. Uncannily similar identifiers, function signatures, control flow, or data structures trigger a rename + restructure, not commentary. The rule is: take concepts, not code. Budget one engineer-day between "read upstream" and "open editor" so the implementation comes from a sketch in Optiqor idioms (`*tenancy.Context` first arg, our `Pricer` / `Recommender` interfaces, our struct names), not from typing alongside the upstream tab.

5. **Narrow vendored primitives are permitted with discipline.** If a specific narrow primitive is genuinely worth vendoring — a CUR row parser, a price-lookup helper, a Prometheus client wrapper that handles a known protocol bug — it goes in `internal/third_party/<component>/` with the upstream `LICENSE` and `NOTICE` files preserved verbatim. The vendored code is not modified; if changes are needed, they go in a wrapper file alongside. This permission applies to narrow integration glue, not to methodology core.

6. **Competitive-landscape framing is fine.** Naming OpenCost, VPA, Karpenter, etc. as ecosystem players in pitch decks, investor material, or competitive analysis is unaffected by this ADR. That's market description, not lineage claim.

## Alternatives considered

**Alternative 1: Permit vendored imports for non-methodology code.**
Allow `go.mod` to depend on OpenCost's CUR parser, VPA's percentile helper, etc., as long as they don't touch the signed methodology. Rejected because: the boundary is fuzzy in practice. A CUR parser feeds the methodology's inputs; a percentile helper is the methodology. Once any of these projects is in `go.mod`, removing it later (when a reviewer asks "why does Optiqor depend on OpenCost") is much harder than not adding it in the first place. The narrow-vendored-primitive escape hatch in Rule 5 covers the cases that actually need it.

**Alternative 2: Per-project policies.**
Different rules for OpenCost (the previous OpenCost-specific posture in `docs/idea.md §4.3`) vs VPA vs Karpenter, calibrated to each project's role. Rejected because: 15 engineers in Year 3 won't remember 5 different policies. One policy is enforceable; five policies is folklore.

**Alternative 3: Surface the lineage publicly.**
"Optiqor builds on OpenCost / VPA / Karpenter" as a stated positioning. Rejected because: it weakens the brand at the moment customers are evaluating whether to trust a signed Receipt. The lineage is real but private; the brand is Optiqor's; both can be true. See the OpenCost-specific posture in `docs/idea.md §4.3` for the strategic reasoning, which this ADR generalizes.

**Alternative 4: No reading allowed; engineers must reinvent everything.**
The strictest possible posture. Rejected because: wasteful and unrealistic. Engineers will read upstream regardless of policy; pretending otherwise creates an enforcement theater rather than a real rule. This ADR's rules describe what engineers do, not what they don't.

## Consequences

**Easier:**
- One uniform policy for the team. New engineers get a single rule, not a per-project decision tree.
- Due-diligence story is simple: "we read upstream privately as reference, our code is our own, no go.mod imports, reviewers enforce structural distinctness." Defensible in an acquisition diligence call, in a SOC 2 audit conversation, in an investor pitch.
- The methodology is meaningfully Optiqor's. Receipt signing remains honest.
- Customer-facing surfaces are clean. No "powered by" footers to retract later.

**Harder:**
- Some engineering work is meaningfully slower than it would be with vendored imports. Writing our own CUR parser when OpenCost has one is a real cost (1-2 engineer-weeks per major component). The hygiene block in [optiqor/todo.md](../../todo.md) Phase 6 documents this for the cost engine specifically.
- Reviewer judgment is required. Structural-similarity is not always obvious; the rule depends on engineers and reviewers exercising care. Mitigation: when in doubt, ask the team — the question itself is a signal that the line is close.
- The narrow-vendored-primitive escape hatch (Rule 5) is judgment-dependent. We accept that judgment cost.

**Locked into:**
- The prohibited-imports CI guard. Adding a project to `prohibited-imports.txt` requires no ADR (operational); removing one requires a new ADR with the rationale and an explicit re-confirmation of the hygiene rules. This guard is the architectural enforcement of this policy.
- Single-policy uniformity. We do not carve out per-project exceptions. If a future project requires different handling, it's a new ADR superseding this one, not a footnote.

**When we'd revisit this:**
- If a customer or regulator explicitly *demands* that Optiqor cite upstream lineage (e.g., a public-sector procurement requirement). Plausible answer: a separate "lineage attestation" artifact, not in the Receipt UI, available on request. Still doesn't require changing the codebase posture.
- If an upstream project's license changes from Apache 2.0 / MIT / BSD to something restrictive, that's a separate issue about whether reading it remains legal — re-examine on a case-by-case basis.
- If Optiqor itself open-sources the methodology via the Year 2+ extraction (per ADR-0006's "When we'd revisit this"), the relationship to upstream projects may evolve — but that's a future ADR, not a relaxation of this one.

## Open questions

- The exact list of projects covered. Initial list: OpenCost, VPA, Karpenter, kube-linter, kubeconform, Goldilocks, Sigstore. Maintained in `.github/lint/prohibited-imports.txt` alongside the CI check.
- Whether kubectl libraries (`k8s.io/client-go`, `k8s.io/api`) count as ecosystem projects under this ADR. They do not — they are Kubernetes API clients, not methodology references. The agent will import client-go in Phase 5 per [optiqor/todo.md](../../todo.md) Phase 5 plans. This ADR does not apply to API client libraries.
- Whether to publish this ADR externally as part of an "engineering culture" page. Lean toward no; it's an internal hygiene rule, not a positioning artifact.

## Cross-references

- [docs/idea.md §4.3](../strategy/idea.md) — the OpenCost-specific private posture this ADR generalizes (internal brain-map).
- [optiqor/todo.md](../../todo.md) Phase 6 — engineering-hygiene block with operational checklist for the cost engine specifically.
- ADR-0006 — methodology library structure that this hygiene protects.
- ADR-0007 — LLM isolation, which is the same kind of architectural integrity claim.

## Implementation status

**Partial.** Policy is in effect from the acceptance date. OpenCost-specific posture is documented at `docs/idea.md §4.3` and `optiqor/todo.md` Phase-6 hygiene block (operational checklist). The unified policy generalized in this ADR has not yet had its CI guard wired — `.github/lint/prohibited-imports.txt` lands with the first `internal/methodology/` PR, alongside a `go list -deps`-based check that fails the build if any of the prohibited imports appear in the dependency graph.

*Last verified: 2026-05-18.*
