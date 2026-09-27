package prom

import (
	"time"

	"github.com/optiqor/optiqor/internal/platform/telemetry"
)

// TelemetryObserver is the production Observer. Registers two metrics
// per query kind on the supplied registry: a duration histogram + an
// error counter (status label coarsened by statusBucket so the
// cardinality stays bounded). nil registry returns a no-op observer.
type TelemetryObserver struct {
	queryLatency *queryHistByKind
	queryErrors  *queryCounterByKindStatus
}

type queryHistByKind struct {
	query      telemetry.Histogram
	queryRange telemetry.Histogram
}

type queryCounterByKindStatus struct {
	reg *telemetry.Registry
	// Each {kind,status} pair gets its own Counter on demand. Idempotent
	// per the Registry's NewCounter contract.
}

func NewTelemetryObserver(reg *telemetry.Registry) *TelemetryObserver {
	if reg == nil {
		return nil
	}
	buckets := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	hist := &queryHistByKind{
		query:      reg.NewHistogram("optiqor_agent_prom_query_seconds", "Per-PromQL latency", map[string]string{"kind": "query"}, buckets),
		queryRange: reg.NewHistogram("optiqor_agent_prom_query_seconds", "Per-PromQL latency", map[string]string{"kind": "query_range"}, buckets),
	}
	return &TelemetryObserver{
		queryLatency: hist,
		queryErrors:  &queryCounterByKindStatus{reg: reg},
	}
}

func (o *TelemetryObserver) ObserveQuery(kind, status string, latency time.Duration) {
	if o == nil {
		return
	}
	switch kind {
	case "query":
		o.queryLatency.query.Observe(latency.Seconds())
	case "query_range":
		o.queryLatency.queryRange.Observe(latency.Seconds())
	}
	if status != "ok" && o.queryErrors != nil {
		c := o.queryErrors.reg.NewCounter(
			"optiqor_agent_prom_errors_total",
			"PromQL queries that did not return 200",
			map[string]string{"kind": kind, "status": status},
		)
		c.Inc()
	}
}
