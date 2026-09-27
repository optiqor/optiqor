# ADR-0004: Workload identity via stable identity hash

**Status:** Accepted
**Date:** 2026-05-18
**Domain:** Data

## Context

A "workload" in Kubernetes (a Deployment, StatefulSet, DaemonSet, etc.) is more fluid than it looks. The same logical workload can:

- Be renamed (`checkout-api` → `checkout-service`)
- Be recreated with a new UID (deleted and reapplied via Helm or kubectl)
- Move between namespaces during refactors
- Be deployed by a different Helm release name while serving the same role

Optiqor needs to attach 30-day rolling history to a workload: its usage patterns, its recommendations, its receipt history, its anomaly baseline. If we key off the workload's *name*, every rename or recreate resets the history. Customers see Optiqor "forget" workloads it should remember.

The K8s `metadata.uid` is stable for the lifetime of an object but resets when the object is deleted and recreated. So `uid` isn't enough either.

This is a decision that **cannot be retrofitted**. If we get the identity model wrong early and accumulate 6 months of metric data keyed off the wrong field, the only fix is to discard that data and start over.

## Decision

Every workload has a stable **identity hash**, computed deterministically from properties that survive renames and recreates:

```
identity_hash = sha256(
  cluster_id ||
  namespace ||
  workload_kind ||
  canonical_selector_labels
)
```

Where `canonical_selector_labels` is the workload's primary selector labels (e.g., `app.kubernetes.io/name`, `app`, or the first `matchLabels` entry), sorted and concatenated in a fixed format.

The agent computes this hash for every workload it sees and reports it. The backend keys workloads on `(cluster_id, identity_hash)` as the natural primary key for upserts — the `workloads` table has `UNIQUE (cluster_id, identity_hash)`.

This means:
- A rename (`checkout-api` → `checkout-service`) does **not** change the identity hash, because selector labels are unchanged. History persists.
- A delete-and-recreate of the same logical workload **does not** change the hash. History persists.
- A genuine new workload (different selector labels) **does** get a new hash. New row.

When a workload's selector labels do change (rare, usually a refactor), the agent reports a different hash; this creates a new row. The old row is preserved with its history; future work can include a "merge workloads" UI for these cases.

The `container_image` field is captured separately on the workloads table (also non-retrofittable; see open questions) and is what drives the cross-customer pattern library.

## Alternatives considered

**Alternative 1: Use `metadata.uid` as the identity.**
Simple. Rejected because: `uid` is destroyed on workload recreation. Helm upgrades that recreate the Deployment object reset the uid. History would be lost on every redeploy.

**Alternative 2: Use `(namespace, name, kind)` as identity.**
Simple, persists across uid changes. Rejected because: renames break it; the example above (`checkout-api` → `checkout-service`) would split the history.

**Alternative 3: Customer manually maps logical workloads.**
Customer tells Optiqor "these three workloads are the same thing." Rejected because: friction during onboarding; brittle as customers refactor; doesn't scale.

**Alternative 4: ML-based workload matching (cluster similar workloads by metrics).**
Tempting later. Rejected for primary identity because: nondeterministic, hard to explain, hard to audit ("why did Optiqor merge these?"). Could be used as a hint for manual review, but not as the primary identity mechanism.

## Consequences

**Easier:**
- Workload history survives renames and recreations.
- The same identity hash on five clusters lets us identify cross-cluster workload families (the `workload_classes` mechanism in ADR-0003's schema).
- The agent does the hashing locally; no network roundtrip needed for identity resolution.

**Harder:**
- The hash function is fixed. If we ever need to change it, we need a migration path: keep old hashes for historical data, add new hashes for new data, link them via a translation table. We will deliberately *not* change the hash function except for genuinely critical reasons.
- Workloads with unusual or missing selector labels need a fallback. **Decision: if no recognizable selector labels exist, fall back to hashing `(cluster_id, namespace, name)` — accept the history-on-rename limitation for these edge cases.** Customers will be a small minority.
- Customers who refactor selector labels will get a new identity hash and "lose" history for that workload until we ship a workload-merge UI. Year 1 acceptable; document the trade-off in customer-facing docs.

**Locked into:**
- The hash function itself, once shipped, cannot change without elaborate migration. We document the hash specification in the public methodology page so customers can verify it themselves.
- The `container_image` field — captured on the workloads table from day one — must always be populated, because the pattern-library moat depends on it. Engineering rule: any code path that creates a workload row must include `container_image` (which can be NULL in pathological cases, but must be attempted).

**When we'd revisit this:**
- Only if we find a real-world identity case that the current hash genuinely cannot handle. Likely never.

## Open questions

- The precise canonical-selector-label algorithm: which labels to prefer when multiple are present, how to handle workloads with no labels (rare but exists). Document in the methodology spec, version it, and never silently change the algorithm.
- Whether to expose the identity hash to customers in the UI. Lean toward yes for advanced views, but not in the default dashboard.

## Implementation status

**Shipped.** `workload_hash BYTEA` column on `workloads` table; populated per the algorithm spec. Tracked complete in `optiqor/todo.md`. The agent that populates this column at observation time is itself a Phase-5 stub (`cmd/agent/main.go`); rows ingested via the Phase-2 sandbox path do compute the hash.

*Last verified: 2026-05-18.*
