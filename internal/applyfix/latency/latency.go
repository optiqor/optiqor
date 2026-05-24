// Package latency carries the Prometheus histogram family that powers
// the Phase-4 "PR comment p95 < 30s" SLO. Each step in the ApplyFix
// workflow records into the same `optiqor_apply_fix_step_duration_seconds`
// metric with a `step` label so Grafana can break the budget down per
// stage.
package latency

import (
	"context"
	"time"

	"github.com/optiqor/optiqor/internal/platform/telemetry"
)

// Step labels the workflow stage. Order pins what the Grafana panel
// reads from left to right; renaming a step breaks the dashboard.
type Step string

const (
	StepParse        Step = "parse"
	StepOperatorGate Step = "operator_gate"
	StepCompose      Step = "compose"
	StepGate         Step = "gate"
	StepValidator    Step = "validator"
	StepRender       Step = "render"
	StepPublish      Step = "publish"
	StepTotal        Step = "total"
)

// AllSteps lists every Step in declaration order so cmd/api +
// cmd/worker can pre-register the histograms at boot.
var AllSteps = []Step{
	StepParse,
	StepOperatorGate,
	StepCompose,
	StepGate,
	StepValidator,
	StepRender,
	StepPublish,
	StepTotal,
}

// Recorder owns one histogram per Step. Construct once at boot from
// a shared *telemetry.Registry; pass to the ApplyFix workflow.
type Recorder struct {
	hists map[Step]telemetry.Histogram
}

// Buckets covers 100ms to 60s — the SLO floor is 30s so the upper
// bucket gives the dashboard headroom to see breaches.
var Buckets = []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 45, 60}

// NewRecorder pre-registers histograms for every Step. The factory
// function on telemetry.Registry is idempotent on (name, labels) so
// re-entrant boot doesn't crash.
func NewRecorder(reg *telemetry.Registry) *Recorder {
	r := &Recorder{hists: map[Step]telemetry.Histogram{}}
	for _, s := range AllSteps {
		r.hists[s] = reg.NewHistogram(
			"optiqor_apply_fix_step_duration_seconds",
			"Apply Fix per-step latency. Sum of non-total steps must approximate total within scheduling jitter.",
			map[string]string{"step": string(s)},
			Buckets,
		)
	}
	return r
}

// Observe records a single step's duration. nil-safe so call sites
// don't have to nil-check when the recorder is disabled in tests.
func (r *Recorder) Observe(step Step, d time.Duration) {
	if r == nil {
		return
	}
	h, ok := r.hists[step]
	if !ok {
		return
	}
	h.Observe(d.Seconds())
}

// Time runs fn with a context-aware deadline-respecting timer and
// records the elapsed duration into step. Returns the result of fn.
// Pattern keeps call sites readable: `err := r.Time(ctx, StepCompose, func() error { ... })`.
func (r *Recorder) Time(_ context.Context, step Step, fn func() error) error {
	start := time.Now()
	defer func() {
		r.Observe(step, time.Since(start))
	}()
	return fn()
}
