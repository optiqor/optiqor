# ADR-0012: Coexist with VPA, HPA, and Karpenter — do not replace them

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Product

## Context

Modern Kubernetes clusters routinely run three autoscaling primitives:

- **HPA (Horizontal Pod Autoscaler)** — built into K8s; scales replica counts based on observed metrics.
- **VPA (Vertical Pod Autoscaler)** — Kubernetes project; recommends or applies CPU/memory requests.
- **Karpenter** — AWS-led open-source node autoscaler; increasingly the default in modern AWS clusters.

A young company entering the K8s cost space has a tempting reflex: build replacements for some or all of these to differentiate. Cast AI famously did this — they ship their own autoscaler that replaces Cluster Autoscaler / Karpenter. This took them years of engineering and remains a maintenance burden.

The question for Optiqor: are these primitives infrastructure we *replace* or infrastructure we *sit above*?

The answer determines the engineering scope, the adoption story, and the competitive positioning.

## Decision

**Optiqor coexists with VPA, HPA, and Karpenter; it does not replace them.** Optiqor is the *intelligence layer* that decides what values these primitives should run with. The primitives themselves are part of the customer's Kubernetes deployment and Optiqor doesn't touch them at runtime.

Specifically:

- **For HPA:** Optiqor reads the HPA spec from the K8s API to detect HPA-managed workloads. Sizing math for HPA-managed workloads is different from static-replica workloads (handled by the methodology). Optiqor may *recommend* changes to HPA configuration (e.g., target utilization, min/max replicas) as PRs, but does not modify HPA at runtime.
- **For VPA:** If VPA is installed in `Off` mode (recommendations only), Optiqor reads VPA's recommendations as one input signal among many. Optiqor's own recommendation, computed by the methodology, is the source of truth — VPA's recommendations are reference data. If VPA is in `Auto` mode (rare in production), Optiqor disables itself for workloads VPA actively manages and notifies the customer.
- **For Karpenter:** Optiqor's cost attribution understands Karpenter consolidation events (pods moving between nodes mid-hour). Optiqor may recommend changes to Karpenter NodePool configurations via PRs (e.g., adding cheaper instance types to the allowed list), but does not modify Karpenter at runtime.

The architectural invariant: **Optiqor does not have RBAC to modify these primitives or any other cluster resource.** It can only recommend changes by opening PRs in the customer's repository. This is enforced by ADR-0008's read-only agent.

Optiqor's own components that *could* be considered "alternative primitives" — the cost attribution engine, the statistical sizing engine, the auto-rollback guard, the receipt signing service — are deliberately built as Optiqor's proprietary methodology, not as drop-in replacements for VPA/HPA/Karpenter. They serve a different purpose: PR-time decision support with signed proof, rather than runtime cluster control.

## Alternatives considered

**Alternative 1: Replace Karpenter with Optiqor's own node autoscaler.**
The Cast AI approach. Rejected because: Karpenter is genuinely good and AWS-supported; competing on bin-packing math against AWS's own engineering team is a losing fight; replacing Karpenter would require customers to uninstall production infrastructure, a multi-month enterprise decision; engineering cost is enormous and ongoing.

**Alternative 2: Replace VPA with Optiqor's own in-cluster vertical autoscaler.**
Tempting because VPA's recommender is genuinely weak (no HPA awareness, no lognormal memory handling). Rejected because: a runtime autoscaler that modifies the cluster directly violates ADR-0008 (read-only agent) and ADR-0009 (all changes through Git). Optiqor's better sizing math is delivered via PRs, not via runtime mutation.

**Alternative 3: Replace HPA with Optiqor's own horizontal autoscaler.**
Rejected because: HPA is part of core K8s; customers depend on it transitively through every Helm chart they install. Replacing it is operationally infeasible.

**Alternative 4: Build alternative primitives but keep them optional.**
Rejected because: would fragment engineering attention, send a confusing product message ("are you a recommender or an autoscaler?"), and replicate Cast AI's engineering burden without their resources. Better to be the recommender for everyone's existing primitives than the alternative primitive for a few.

## Consequences

**Easier:**
- Adoption is fast: customers don't have to uninstall anything; Optiqor adds value without changing their existing infrastructure.
- Engineering scope is bounded: we don't have to build, maintain, and support our own autoscaler.
- Positioning is sharp: "Optiqor sits above your autoscaling primitives, making them smarter." Different from Cast AI's "replace them" pitch and different from Kubecost's "watch them" pitch.
- Composability: customers can run Optiqor *and* Cast AI if they want (we recommend at PR time; Cast AI optimizes the running cluster). This sometimes turns "competitor" framing into "complementary" framing in sales conversations.

**Harder:**
- We benefit from upstream improvements (Karpenter getting better helps customers running Optiqor); we don't differentiate on those features ourselves. **This is fine; our differentiation is the PR-time + receipt layer, not the autoscaling primitive.**
- For some customer use cases, the right answer is "what you really need is better node provisioning" — and we can recommend the customer adopt Karpenter, but we can't *be* Karpenter for them.
- We must stay current with VPA/HPA/Karpenter evolution. When Karpenter ships a new feature, our recommendation engine must understand it. Ongoing maintenance, but small.

**Locked into:**
- The "intelligence layer, not autoscaling primitive" positioning. Reversing it would mean acquiring or building a real autoscaler — a multi-year project.
- Coexistence requires that we *read* state from these primitives reliably. The agent's RBAC (ADR-0008) must include access to VPA, HPA, and Karpenter custom resources. Already specified.

**When we'd revisit this:**
- In Year 3+, if a specific customer requirement (e.g., a regulated customer who needs deterministic resource control at the runtime layer with cryptographic attestation) cannot be solved through PR-time recommendations. At that point, we might build an in-cluster controller as an *optional* extension — never a replacement for the customer's existing autoscaling, but an additional layer for specific use cases.
- Never to compete with Karpenter on pure node provisioning.

## Open questions

- The exact PR template for recommending Karpenter NodePool changes vs. workload changes (different files, different review needs). Implementation detail; defer.
- How aggressively to integrate with custom autoscalers (e.g., KEDA for event-driven scaling). Year 2 question.
- When a customer has VPA, HPA, AND Karpenter all configured and a workload triggers a recommendation that might conflict with one or more of them, what's the resolution logic? Document in the methodology spec.

## Implementation status

**Partial.** Schema scaffolding shipped: `tenants.node_provisioner_class` enum in `migrations/0001_baseline.sql:93` (karpenter / autoscaler / static / managed-*); `workloads.has_hpa` boolean in `migrations/0002_workload_observed_state.sql:33`. **Not yet shipped:** integration packages `internal/agent/k8s/{vpa,hpa,karpenter}` for reading VPA recommendations, HPA spec, and Karpenter NodePool config from the cluster API. All three depend on the Phase-5 agent watch loop. VPA presence isn't even tracked in `optiqor/todo.md` yet — needs a new Phase-4 or Phase-5 line item.

*Last verified: 2026-05-18.*
