# Agent install

The in-cluster agent ships K8s + Prometheus + provisioner data to the
SaaS over mTLS every 60s. This page is for an operator setting up the
agent for the first time.

## What you need before `helm install`

- A Kubernetes cluster on 1.28+ (we test 1.31 in CI).
- A reachable Prometheus (any kube-prom-stack or vanilla Prom install).
  Optional, but missing it gates the validator's right-sizing math.
- A mTLS client certificate the agent uses to authenticate to the SaaS
  ingest. You provision this either via cert-manager + `Certificate`
  CRD, or by handing the cert to us out-of-band and mounting it as a
  Secret.
- A Helm 3.13+ install on the operator's laptop.

## Provisioner support

The agent's `internal/agent/provisioner` detector classifies the
cluster's node-provisioning shape on first snapshot. Supported in
Year 1:

| Class | Detection |
|---|---|
| `karpenter` | NodePool CRD present |
| `autoscaler` | `cluster-autoscaler` Deployment in `kube-system` |
| `static` | neither of the above |

Unsupported provisioners (GKE Node Auto-Provisioning, OpenShift
Machine API, DigitalOcean managed K8s, etc.) are detected by the
pre-flight checker and **fail closed**. The installer rejects the
install with an explicit error and a tracking-issue link rather than
silently routing the cluster to `static`. See `0334` in the repo
todo.md for context.

## Pre-flight

Run the pre-flight before `helm install` — it surfaces RBAC + Prom +
provisioner gaps in 6s without touching the cluster's namespaces:

```
POST /v1/onboarding/preflight
Content-Type: application/json

{ "prometheus_url": "http://prometheus-operated.monitoring:9090" }
```

The response is a checklist; the wizard at `/install/preflight` renders
the same list with green / amber / red pips.

## Install

```
helm install optiqor-agent ./deploy/helm/optiqor-agent \
  --namespace optiqor-agent --create-namespace \
  --set prometheus.url=http://prometheus-operated.monitoring:9090 \
  --set mtls.secretName=agent-tls
```

Within 60s the agents row appears in the dashboard; the Agent Health
pill turns green. If it doesn't, see [agent-rbac.md](agent-rbac.md)
first, then [prometheus.md](prometheus.md).

## Verify

- Dashboard `/app` shows the agent's pill as green.
- `kubectl logs -n optiqor-agent deploy/optiqor-agent` ends with
  `snapshot posted ok` every 60s.
- The cluster row's `node_provisioner_class` column matches what's
  installed (kubectl get nodepools, kubectl get deploy
  cluster-autoscaler).

## Rotate the mTLS cert

Cert lifetime is 90 days. cert-manager rotates automatically. If the
agent reports `tls: bad certificate` for >5 minutes, the cert went
stale. Rotate the underlying Secret and the agent picks up the new
cert on the next 60s tick (no restart needed).
