# ADR-0008: Agent — read-only K8s, outbound-only network

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Agent

## Context

The Optiqor agent runs inside the customer's Kubernetes cluster. From a security perspective, it is one of the highest-leverage pieces of software in Optiqor: a compromise of the agent could, in principle, be used to read sensitive metadata, modify production workloads, or pivot into the customer's network.

Customers — especially regulated ones — will scrutinize the agent's permissions and network footprint heavily during vendor security review. The agent's posture must be defensible, minimal, and consistent with how trusted observability agents (Datadog, Grafana Agent, etc.) operate.

The architectural question is not just "what does the agent need to function" but "what does the agent need *and no more*, so that even if compromised, the blast radius is bounded."

## Decision

The Optiqor agent operates under two strict principles:

**Principle 1: Read-only access to Kubernetes.** The agent has RBAC permissions to read workload metadata, services, events, ConfigMaps, and Pods. It can also query Prometheus or kube-state-metrics. **It has zero write permissions on the K8s API.** It cannot create, modify, or delete any cluster resource. All cluster changes Optiqor causes flow through Git (see ADR-0009).

**Principle 2: Outbound-only network.** The agent initiates connections to Optiqor's backend over HTTPS. **Optiqor never initiates inbound connections to the agent or the customer's cluster.** No firewall holes, no VPN, no peering, no inbound port exposure.

Specific RBAC the agent needs (Year 1 scope):
- `get`, `list`, `watch` on: pods, services, deployments, statefulsets, daemonsets, replicasets, jobs, cronjobs, namespaces, nodes, persistentvolumeclaims, horizontalpodautoscalers, poddisruptionbudgets, resourcequotas, limitranges, configmaps (selected, for Helm release detection)
- `get`, `list`, `watch` on `verticalpodautoscalers` (CRD) if VPA is installed — to read its recommendations as input
- `get`, `list`, `watch` on Karpenter CRDs if Karpenter is installed
- Connection to Prometheus (HTTP, in-cluster service)
- No access to: secrets, service accounts, role bindings, validating/mutating webhooks, anything that could be used for privilege escalation
- No access to events that might leak sensitive data (TBD: review whether `events` itself should be excluded)

**The agent never reads Secrets.** Even in read mode. The Helm release information the agent needs comes from ConfigMaps and labels, not from Helm's secret-stored release records (we accept this Year 1 limitation; some Helm-stored data is unavailable to Optiqor as a result).

The Dry-Run validation in ADR-0010 requires the agent to perform `kubectl --dry-run=server` on Optiqor's behalf. `--dry-run=server` does not modify cluster state; the agent's read-only role is sufficient (we verify this assumption with K8s docs and tests).

## Alternatives considered

**Alternative 1: Read-write agent (the "we can do everything" model).**
Cast AI's approach. Agent has create/delete/modify permissions on nodes and workloads. Acts on the cluster directly. Rejected because: (a) violates Optiqor's positioning (GitOps-native, every change a commit), (b) customer security posture; many customers will not install such an agent in production, (c) makes the agent a higher-stakes target. The whole strategy depends on *not* doing this.

**Alternative 2: Read-only Kubernetes, but inbound network allowed.**
Lets Optiqor's backend "push" commands to the agent (e.g., "validate this diff now"). Rejected because: inbound network is enterprise-adoption poison. Even one customer with a security team will require many weeks of negotiation. Modern observability agents have proven outbound-only is the right model.

**Alternative 3: No agent at all — backend pulls from Prometheus/K8s via the public internet.**
Rejected because: requires customer to expose Prometheus and K8s API to the internet, which is a much worse security posture than running a read-only agent in-cluster.

**Alternative 4: Sidecar-injected agent per workload.**
Rejected because: enormous footprint (one agent process per workload), invasive to customer's deployment model. The single-agent-per-cluster model is the right footprint.

## Consequences

**Easier:**
- Customer security review: short. The agent does much less than alternatives; the threat model is bounded.
- The "worst case if Optiqor is compromised" story is genuinely small: an attacker reads workload metadata. They cannot modify production. This is a real security feature, not just marketing.
- Outbound-only network works with every enterprise firewall configuration and air-gap proxy setup; no special networking required.
- Operational simplicity: one agent process per cluster, no inbound port management.

**Harder:**
- Some optimization opportunities require knowing what's in Secrets (e.g., reading the database password to test connection pool sizing). We don't do these. **Accepted limitation; revisit if it becomes a customer-blocking gap.**
- Dry-run validation requires a round-trip through the agent (~1-3 second added latency on PR analysis). Acceptable; we cache validation results.
- We can't push real-time updates to the agent (e.g., "stop analyzing this workload, customer just deleted it"). Agent polls periodically or uses long-poll patterns. Acceptable for our use case.
- Some K8s primitives (Helm release info in Secrets, certain CRD secrets) are invisible to us. We document this and provide workarounds (ConfigMap-based release labeling).

**Locked into:**
- The RBAC scope, once shipped, can only be expanded with care. Adding new permissions in a Helm chart upgrade may trigger customer security re-review. We document any future scope additions clearly.
- The outbound-only model. We cannot ever add features that require inbound. (We don't want to.)

**When we'd revisit this:**
- If a customer use case genuinely cannot be solved without write permission on the cluster *and* they explicitly opt into a write-mode agent for non-prod environments. Even then, a write-mode agent would be a separate optional component, not the default.
- Never reconsider the outbound-only network rule. It's foundational.

## Open questions

- Exact list of K8s events the agent reads, and how to redact sensitive event data. Operational detail; document in agent's privacy spec.
- Whether to support a "thin" mode (just data forwarding) vs "fat" mode (some detection on-cluster) for air-gapped customers. Year 2 question.
- Bandwidth budget: how much data does the agent send per cluster per day? Target: <100MB/day for an average 200-workload cluster. Validate post-launch.

## Implementation status

**Not yet shipped.** `cmd/agent/main.go` is a 46-line stub that initializes logging and idles on SIGTERM. Real watch loop (client-go informers + Prometheus scrape + mTLS to SaaS, outbound-only) is Phase-5 work per `optiqor/todo.md:262`. The Tier-1 data-source readers (`internal/agent/k8s/{events,hpa,policy}`) are also Phase-4/5 unchecked items. The architectural rules in this ADR will be enforced by the agent's Helm chart (NetworkPolicy + ServiceAccount + RBAC) when the watch loop lands.

*Last verified: 2026-05-18.*
