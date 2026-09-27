package prom

import "time"

// Canonical is the PromQL set the agent runs. Two shapes:
//
//   - Live (instant): five-minute rate / current value. Sized for the
//     60-second snapshot loop; the validator pipeline reads these.
//   - Historical (range): 30-day percentile roll-ups computed by the
//     customer's Prometheus via quantile_over_time. The statistical
//     sizing engine consumes these.
//
// Every query filters container!="POD", container!="" so cAdvisor
// pause-container overhead doesn't pollute usage numbers — the trap the
// research doc names as causing 20-40% over-recommendation when
// missed.
//
// Memory queries use container_memory_working_set_bytes (the metric the
// OOM killer consults), never container_memory_usage_bytes (which
// includes reclaimable page cache and over-attributes).
var Canonical = struct {
	// Live (5m rate / current).
	CPURate           string
	MemoryWorkingSet  string
	OOMKilledIncrease string
	OOMWindow         time.Duration

	// 30-day percentiles. Step is captured separately because the
	// caller composes Range — these queries pre-bucket via the subquery
	// shape [30d:1h] so the customer's Prometheus does the heavy
	// downsample work, not the agent.
	CPURateP95_30d       string
	CPURateP99_30d       string
	MemoryWorkingP99_30d string
	HistoricalStep       time.Duration

	// kube-state-metrics canonical set. The agent reads these alongside
	// client-go for cross-source verification — the doc's "customer's
	// engineer must verify by running the same PromQL in their Grafana"
	// property only holds if we read what they read.
	HPAMinReplicas       string
	HPAMaxReplicas       string
	HPACurrentReplicas   string
	HPATargetUtilization string
	PodRequestsCPU       string
	PodRequestsMemory    string
	PodLimitsCPU         string
	PodLimitsMemory      string
	PodRestarts          string
}{
	CPURate:           `sum by (namespace,pod) (rate(container_cpu_usage_seconds_total{container!="POD",container!=""}[5m]))`,
	MemoryWorkingSet:  `sum by (namespace,pod) (container_memory_working_set_bytes{container!="POD",container!=""})`,
	OOMKilledIncrease: `sum by (namespace,pod) (increase(kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}[7d]))`,
	OOMWindow:         7 * 24 * time.Hour,

	CPURateP95_30d: `quantile_over_time(0.95, ` +
		`sum by (namespace,pod) (rate(container_cpu_usage_seconds_total{container!="POD",container!=""}[5m]))` +
		`[30d:1h])`,
	CPURateP99_30d: `quantile_over_time(0.99, ` +
		`sum by (namespace,pod) (rate(container_cpu_usage_seconds_total{container!="POD",container!=""}[5m]))` +
		`[30d:1h])`,
	MemoryWorkingP99_30d: `quantile_over_time(0.99, ` +
		`sum by (namespace,pod) (container_memory_working_set_bytes{container!="POD",container!=""})` +
		`[30d:1h])`,
	HistoricalStep: time.Hour,

	HPAMinReplicas:       `kube_horizontalpodautoscaler_spec_min_replicas`,
	HPAMaxReplicas:       `kube_horizontalpodautoscaler_spec_max_replicas`,
	HPACurrentReplicas:   `kube_horizontalpodautoscaler_status_current_replicas`,
	HPATargetUtilization: `kube_horizontalpodautoscaler_spec_target_metric`,
	PodRequestsCPU:       `kube_pod_container_resource_requests{resource="cpu"}`,
	PodRequestsMemory:    `kube_pod_container_resource_requests{resource="memory"}`,
	PodLimitsCPU:         `kube_pod_container_resource_limits{resource="cpu"}`,
	PodLimitsMemory:      `kube_pod_container_resource_limits{resource="memory"}`,
	PodRestarts:          `kube_pod_container_status_restarts_total`,
}

// HistoricalRange builds the Range the 30-day percentile queries
// consume. End defaults to now (caller passes their injected clock).
// Centralised so a future change to the 30d window is one edit, not
// scattered through callers.
func HistoricalRange(now time.Time) Range {
	return Range{
		Start: now.Add(-30 * 24 * time.Hour),
		End:   now,
		Step:  Canonical.HistoricalStep,
	}
}
