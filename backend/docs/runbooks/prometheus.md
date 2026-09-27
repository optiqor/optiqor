# Prometheus not reachable

The pre-flight checker couldn't reach the Prometheus URL the install
wizard was given. Without Prometheus the agent ships the K8s inventory
+ provisioner class but no per-workload metrics; the validator's
right-sizing math falls back to request/limit ratios only.

## Verify

From inside the cluster:

```
kubectl run -it --rm probe --image=curlimages/curl --restart=Never -- \
  -s http://prometheus-operated.monitoring:9090/-/ready
```

Expect `Prometheus is Ready.\n`. Any other response indicates one of:

- Service name mismatch. kube-prometheus-stack ships
  `prometheus-operated` in the `monitoring` namespace by default;
  vanilla Prom ships `prometheus-server` in `prometheus`.
- Network policy blocks egress from the agent namespace to the Prom
  namespace.
- Prom is on Thanos sidecar mode and the query API isn't local —
  point the agent at the Thanos Querier (typically
  `thanos-query.monitoring:10902`).

## Fix

Hand the right URL to the agent via the Helm value:

```
helm upgrade optiqor-agent ./deploy/helm/optiqor-agent \
  --reuse-values \
  --set prometheus.url=http://prometheus-operated.monitoring:9090
```

If the install was already done with the wrong URL, re-run the
pre-flight after the upgrade; the check should flip to pass without
restarting the agent pod.

## Operating without Prometheus

The agent boots fine with no Prometheus URL set. The dashboard's
"Prometheus reachable" check stays at warn (not fail) and the
validator pipeline runs without metrics-derived signals. New
right-sizing PRs will be tagged `low confidence` and require manual
approval. Long-term, install Prom or point at an existing one to
restore the canonical confidence math.
