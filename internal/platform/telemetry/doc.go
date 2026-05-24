// Package telemetry holds Prometheus metrics + OpenTelemetry tracing
// (Tempo via OTLP) for api, worker, and agent. Metric names follow
// optiqor_<domain>_<metric>_<unit>; deviations break dashboards.
package telemetry
