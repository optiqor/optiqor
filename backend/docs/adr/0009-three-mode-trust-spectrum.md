# ADR-0009: Three-mode trust spectrum, all changes through Git

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Product

## Context

The K8s cost market is polarized along an autonomy axis. Cast AI and ScaleOps operate autonomously: they apply changes directly to the cluster, often bypassing the customer's Git source of truth. Kubecost and OpenCost are at the other end: they show dashboards, do nothing automatically. Customers consistently report (per CloudBolt's March 2026 research on the Kubernetes Automation Trust Gap) that they want automation but distrust black-box autonomous rightsizing for production.

Two architectural questions follow:

1. **What's the right autonomy model for Optiqor?** A binary "PR-only or autopilot" choice mirrors the market polarization; that's exactly the wrong response to the trust gap.
2. **What's the right relationship to the customer's GitOps pipeline?** Argo CD and Flux are the dominant GitOps tools. Cluster changes that bypass Git create reconciliation conflicts; changes that flow through Git compose naturally.

Both questions have a single coherent answer.

## Decision

Optiqor offers a **three-mode trust spectrum**, not a binary:

1. **Suggest mode** — Optiqor displays findings in the dashboard only. No PRs. No cluster changes. Engineers can manually act on findings if they choose. Default for new customers and for environments where the customer wants observation only.

2. **Propose mode (the hero)** — Optiqor opens Apply Fix PRs into the customer's repository. A human engineer reviews and merges. The customer's GitOps pipeline (Argo CD, Flux, or kubectl-apply CI) then reconciles the merged change into the cluster. **Optiqor never modifies the cluster directly.** This is the default for production environments.

3. **Auto-merge mode** — Optiqor opens an Apply Fix PR and *itself* merges the PR after the validation gate (ADR-0010) and blast-radius checks pass. The change still flows through Git as a commit; the customer's GitOps tool still reconciles it. The only difference from Propose mode is who clicks merge. **Opt-in per environment, never default. Recommended scoping: dev/staging only. Production stays in Propose mode unless explicitly authorized.**

The architectural invariant across all three modes: **every change Optiqor causes is a Git commit signed by `optiqor[bot]` in the customer's repository.** Optiqor never bypasses the customer's source of truth. There is no fourth "direct-to-cluster autopilot" mode and there will never be one.

This is enforced by ADR-0008: the agent has no write permissions on the cluster. Optiqor cannot bypass Git even if it wanted to, because the only way to change the cluster is through the customer's own GitOps reconciliation.

## Alternatives considered

**Alternative 1: Binary autopilot vs PR-only.**
The market's mental model. Rejected because: the binary itself is the trap. Customers don't want either extreme. The three-mode spectrum reflects the actual gradient of trust customers report.

**Alternative 2: Direct-to-cluster autopilot (Cast AI's model).**
Optiqor's agent gets write permissions; changes are applied to the cluster directly. Rejected because: violates ADR-0008 (read-only agent), destroys the "every change is auditable in Git" property, creates Argo CD/Flux reconciliation conflicts, and abandons the differentiated positioning. **Non-starter.**

**Alternative 3: Auto-merge mode but with shorter delay (e.g., merge instantly without validation gate).**
Faster, but unsafe. Rejected because: ADR-0010's validation gate is what protects us from Kubecost's PR Action failure mode. Skipping it for "speed" trades the entire product's safety story for milliseconds.

**Alternative 4: A "review window" instead of auto-merge — Optiqor proposes auto-merge, engineer has N hours to veto.**
Considered as a middle option. Rejected because: ambiguous accountability if the change breaks something; the engineer says "I didn't review it" and Optiqor says "you had 4 hours." Either Optiqor takes the responsibility (auto-merge) or the engineer does (Propose). No middle ground.

## Consequences

**Easier:**
- Customers self-select their trust level. Conservative shops live in Suggest. Mid-comfort shops live in Propose. Confident shops graduate parts of their footprint to Auto-merge.
- Sales conversation handles the trust gap directly: "We meet you at your trust level. Start in Suggest. Move to Propose when you trust the recommendations. Move dev/staging to Auto-merge when you trust the workflow."
- Argo CD / Flux integration is automatic: Optiqor's changes are commits like any other. The customer's existing GitOps pipeline reconciles them with the same audit trail, same rollback path, same approval process as everything else.
- The differentiation from Cast AI/ScaleOps is structural, not marketing: "We never bypass your Git" is true by construction (ADR-0008).

**Harder:**
- Three modes is more product surface than one. Documentation, UI, and onboarding must explain the modes clearly.
- The blast-radius gate (for Auto-merge mode) requires per-environment safety scoring. This is real engineering work (specced as one of the eight architectural gaps).
- "Auto-merge through Git" is slightly slower than "direct to cluster" — typically 30-90 seconds for the GitOps reconciliation. **Acceptable cost; sells as a feature ("audit trail, revertible, GitOps-respecting").**

**Locked into:**
- The "all changes through Git" invariant. Reversing this would mean asking the agent for write permissions on the cluster, which is foundational. We commit to this forever.
- The three-mode taxonomy. Adding a fourth mode (e.g., "Suggest with daily digest") is fine; *changing* an existing mode's semantics is breaking and requires a new ADR.

**When we'd revisit this:**
- Never for the "all changes through Git" invariant.
- For specific safety policy refinements within Auto-merge mode (blast-radius thresholds, environment scoping rules), as we learn from real customer use.

## Open questions

- The blast-radius scoring algorithm: deferred to a separate spec. Inputs likely include replica count, traffic volume, environment tag, recent change history.
- Whether Auto-merge should require a "cool-off" period between merges to avoid flooding (e.g., max 3 auto-merges per repository per day). Lean toward yes, customer-configurable.
- How Optiqor handles a customer who has no GitOps tool (raw kubectl apply or manual deployment). Their Apply Fix PR merges, but who reconciles it to the cluster? Likely answer: we don't support this customer in Auto-merge mode; Propose mode works fine for them.

## Implementation status

**Not yet shipped.** Env-aware aggressiveness (prod conservative, staging moderate, dev aggressive) lives at `internal/safety/environment/environment.go` — this is a different concept from the per-tenant Suggest/Propose/Auto-merge trust spectrum. Lifecycle states (snooze/dismiss/ignore) tracked unchecked in `optiqor/todo.md` Phase-5 production-readiness section. Per-tenant mode selection + blast-radius scoring + Auto-merge wiring are Phase-5/6 work. **Open coordination question:** how env-aware aggressiveness composes with the trust spectrum mode — must be specified before either reaches production.

*Last verified: 2026-05-18.*
