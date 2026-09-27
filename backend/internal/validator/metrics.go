package validator

import (
	"github.com/optiqor/optiqor/internal/platform/telemetry"
)

// Metrics owns the validator-rejects counter family. Build once at
// boot from a shared registry; pass to the pipeline so rejections
// emit `optiqor_validator_rejects_total{reason=...}`.
type Metrics struct {
	counters map[string]telemetry.Counter
	any      telemetry.Counter
}

// NewMetrics pre-registers a counter per known validator (those
// returned by Default()) plus a fallback "other" bucket so an
// unexpected reason never silently drops.
func NewMetrics(reg *telemetry.Registry) *Metrics {
	m := &Metrics{counters: map[string]telemetry.Counter{}}
	for _, v := range Default() {
		name := v.Name()
		m.counters[name] = reg.NewCounter(
			"optiqor_validator_rejects_total",
			"Validator hard-rejections by reason.",
			map[string]string{"reason": name},
		)
	}
	m.any = reg.NewCounter(
		"optiqor_validator_rejects_total",
		"Validator hard-rejections by reason.",
		map[string]string{"reason": "other"},
	)
	return m
}

// RecordReject bumps the counter for the validator that produced the
// hard verdict. Unknown reasons go to the "other" bucket.
func (m *Metrics) RecordReject(reason string) {
	if c, ok := m.counters[reason]; ok {
		c.Inc()
		return
	}
	if m.any != nil {
		m.any.Inc()
	}
}
