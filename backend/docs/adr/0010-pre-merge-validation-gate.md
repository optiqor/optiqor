# ADR-0010: Pre-merge validation gate — four stages

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Product

## Context

Kubecost shipped a "PR Action" feature that commented on PRs with cost recommendations. It failed in the market. The failure mode, well-documented in public discussion: comments were noisy, recommendations were sometimes invalid against the cluster's actual state, customers turned the integration off.

This is the single most important failure mode for Optiqor to avoid. If Optiqor ever opens a PR that doesn't render, doesn't conform to the cluster's K8s version, or fails admission webhooks, the customer's trust collapses immediately. The CEO of every K8s-aware customer remembers Kubecost's failure; they will not forgive Optiqor for repeating it.

The technical question: before Optiqor opens any Apply Fix PR, how do we *prove* the resulting diff will not break the cluster?

## Decision

Every proposed Apply Fix passes through a **four-stage validation gate** before a PR is opened. If any stage fails, the recommendation is marked `validation_failed`, logged, and silently discarded. The customer never sees a broken PR proposal.

**Stage 1 — Render.** Apply the proposed diff to the customer's manifest source (Helm values, Kustomize overlay, or raw YAML). Run `helm template` or `kustomize build` (or no-op for raw YAML) to produce final K8s manifests. Verify the rendering succeeded with no errors.

**Stage 2 — Conform.** Run `kubeconform` against the rendered manifests, configured for the customer cluster's actual K8s API version (fetched from the agent). Verify all resources are schema-valid for that version. Catches things like deprecated APIs (`extensions/v1beta1`), missing required fields, type mismatches.

**Stage 3 — Lint.** Run `kube-linter` against the rendered manifests for known misconfiguration patterns. Catches things like missing resource limits, privileged containers, hostPath mounts. This stage is also where Optiqor's free "security hints" and "reliability hints" originate.

**Stage 4 — Dry-run server.** Send the rendered manifests to the customer's actual cluster API server via `kubectl --dry-run=server`. The agent performs this call on Optiqor's behalf (agent has read-only RBAC; `--dry-run=server` does not modify state). The cluster's admission webhooks run, including ValidatingAdmissionWebhooks and any custom policy engines (OPA Gatekeeper, Kyverno). Verify all webhooks accept the manifest.

Additionally, **constraint-specific checks** run before stage 4: does the new resource request fit within the namespace's `ResourceQuota`? Does it stay within `LimitRange` bounds? Would it violate any `PodDisruptionBudget` mid-rollout? These are checked by reading the constraints from the K8s API and computing the answer locally; they're fast and don't require a webhook round-trip.

Each stage's result is recorded in the `apply_fix_operations` table with timestamps and detailed failure reasons. Customer dashboard shows the "this recommendation didn't pass validation, here's why" detail for debugging.

## Alternatives considered

**Alternative 1: Trust the methodology; don't validate before opening PRs.**
Kubecost's failed approach. Rejected because: even mathematically-correct recommendations can violate cluster-specific constraints (admission webhooks, ResourceQuotas, custom policies). Trust requires proof.

**Alternative 2: Only render-and-conform, skip the cluster dry-run.**
Faster. Rejected because: the cluster's admission webhooks are where customer-specific policies live. OPA Gatekeeper rules, security policies, namespace conventions — all of these are customer-specific and only enforceable through dry-run against the actual cluster. Skipping this stage misses the customer-specific failure modes that matter most.

**Alternative 3: Open the PR, let the customer's CI catch breakage.**
Theoretically works if the customer has thorough CI. Rejected because: not all customers have CI on every K8s manifest change; if the CI catches it, Optiqor has already burned trust by opening a broken PR; the validation moves from Optiqor's side (where we can iterate) to the customer's side (where we cannot).

**Alternative 4: Asynchronous validation — open the PR, then validate.**
Rejected because: a PR that opens then closes itself in 30 seconds looks worse than no PR.

## Consequences

**Easier:**
- The "we never open a broken PR" promise is structurally true, not aspirational. We can say it confidently in sales conversations.
- Customers don't have to trust Optiqor's word; they can audit the validation pipeline (it's documented and the failure log is visible to them).
- We avoid the Kubecost PR-Action failure mode by construction.
- The validation stage doubles as the source for "security and reliability hints" — stage 3 (`kube-linter`) finds these for free.

**Harder:**
- Validation adds latency to the Apply Fix flow. End-to-end target: <30 seconds from "recommendation generated" to "PR opened." Most of this is the dry-run round-trip through the agent.
- Some recommendations will fail validation, especially in heavily-customized clusters with strict admission webhooks. **We accept that some valuable recommendations cannot be applied. The customer is better served by no PR than by a broken PR.**
- The validation pipeline is itself a fairly complex piece of code. **Mitigation: each stage is independent and testable; integration tests cover known-broken inputs.**
- The agent must support performing dry-run calls on Optiqor's behalf. This is the only "active" thing the agent does (versus passive observation); ADR-0008's permissions are designed to allow it without granting write access.

**Locked into:**
- The four-stage sequence. We can add stages but cannot remove them (removing weakens the safety guarantee). Adding a stage 5 (e.g., custom Optiqor policy checks) is fine.
- The dependency on the cluster's admission webhook layer. If a cluster has no admission webhooks, stage 4 still runs but finds nothing customer-specific. Acceptable.
- The choice of tools: `helm template`, `kustomize build`, `kubeconform`, `kube-linter`, `kubectl --dry-run=server`. These are all Apache 2.0 or open-source. If any becomes unmaintained, we replace with the equivalent.

**When we'd revisit this:**
- If validation false-positives (good recommendations rejected as broken) become a customer complaint, we'd refine specific stages, not remove them.
- If a new K8s feature requires a new validation stage (e.g., a future "policy preview" API), we add it.

## Open questions

- Exact timeout and retry policy for the dry-run round-trip. Implementation detail.
- How to handle clusters with very slow admission webhooks (>10s response time). Document expected webhook latency; provide a fallback path that skips stage 4 with explicit customer opt-in. Lean toward fail-closed: if dry-run times out, the PR is not opened.
- Cache strategy for validation results when the same diff is re-validated. Lean toward caching aggressively, invalidating on any manifest change.

## Implementation status

**Not yet shipped.** None of the four validation stages are wired. `internal/applyfix/gate/{template,kubeconform,validator,dryrun}` are unchecked Phase-4 work in `optiqor/todo.md:242-247`. The dry-run stage depends on the Phase-5 agent for the cluster round-trip — no validation gate without an agent. Apply Fix cannot dispatch to PRs safely until this gate is wired; the codebase has a `prwriter` package ready for the PR-opening step, but the gate is the prerequisite.

*Last verified: 2026-05-18.*
