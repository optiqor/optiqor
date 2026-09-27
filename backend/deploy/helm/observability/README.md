# Optiqor observability stack

> Status: scaffolding — applied to the EKS cluster as part of the
> Phase 1 infrastructure rollout.

This directory holds the observability stack Helm values + Prometheus
recording rules. The stack:

| Component | Chart | Purpose |
| --- | --- | --- |
| Prometheus | `prometheus-community/kube-prometheus-stack` | Metrics scrape + alerts |
| Grafana | (bundled with kube-prometheus-stack) | Dashboards |
| Loki | `grafana/loki-stack` | Log aggregation |
| Tempo | `grafana/tempo` | Distributed tracing |
| OpenTelemetry Collector | `open-telemetry/opentelemetry-collector` | Trace + metric receiver |

Sentry is configured separately via the `OPTIQOR_SENTRY_DSN` env var on
the api/worker binaries (per-env DSN provisioned in Sentry SaaS).

## Apply

```sh
# Add charts
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo update

# Install
helm upgrade --install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  -n observability --create-namespace -f kube-prometheus-stack-values.yaml \
  --set-file additionalPrometheusRulesMap.optiqor=rules/optiqor-slo.yaml
helm upgrade --install loki grafana/loki-stack -n observability -f loki-values.yaml
helm upgrade --install tempo grafana/tempo -n observability -f tempo-values.yaml
helm upgrade --install otel-collector open-telemetry/opentelemetry-collector -n observability -f otel-values.yaml
```

The recording rules in [rules/optiqor-slo.yaml](rules/optiqor-slo.yaml)
materialise the Year-1 SLO targets so dashboards and alerts read the
same numbers committed in `backend/todo.md`.
