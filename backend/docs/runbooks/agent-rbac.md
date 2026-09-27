# Agent RBAC failed

The pre-flight checker reported that the agent's ServiceAccount cannot
`get/list/watch` one or more of the six Tier-1 GVRs. The agent runs
read-only — it never mutates cluster state — so this is always a
permissions miss, not a workload problem.

## Verify

Pre-flight returns a `fail` row named `ServiceAccount RBAC`. Confirm
locally:

```
kubectl auth can-i list events --as system:serviceaccount:optiqor-agent:optiqor-agent
kubectl auth can-i list horizontalpodautoscalers --as system:serviceaccount:optiqor-agent:optiqor-agent
kubectl auth can-i list poddisruptionbudgets --as system:serviceaccount:optiqor-agent:optiqor-agent
```

Any `no` means the bundled ClusterRole did not bind. The most common
causes:

- The ClusterRoleBinding template was edited and the subject
  `ServiceAccount` namespace doesn't match where the chart was
  installed.
- Cluster runs a restrictive OPA / Kyverno policy that denied the
  Optiqor ClusterRole at apply time. Check the admission webhook
  logs.
- The cluster admin pre-installs Optiqor's RBAC out-of-band and the
  Helm release skips it; confirm by inspecting the apply audit log.

## Fix

The canonical ClusterRole ships in
`deploy/helm/optiqor-agent/templates/clusterrole.yaml`. Re-apply with
`helm upgrade --reset-values` to drop any local overrides. Then re-run
the pre-flight; the gate should flip to pass within ~10s of the
ClusterRoleBinding landing.

## Why this is a hard fail (not a warn)

Without read access to events + HPA + PDB the validator pipeline can't
gate the candidate against cluster constraints — a memory cut would
ship without seeing the workload's OOMKilled history, a replica cut
would ship without seeing the PDB minAvailable. The right behaviour is
to refuse to register the agent until the RBAC binds.
