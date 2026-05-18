package classify

import (
	"errors"
	"time"
)

// Class is one of four behavior classes derived from a workload's
// usage time-series. The sizing engine picks a different percentile
// target and safety-margin policy for each class — see ADR-0009 and
// the methodology spec at optiqor.dev/methodology/hybrid-v1.
type Class string

const (
	// ClassSteady — coefficient of variation < 0.2, no significant
	// 24h or 168h autocorrelation. Sizing: P95 + 30% safety margin.
	ClassSteady Class = "steady"

	// ClassDailyCyclical — significant autocorrelation at the 24-hour
	// lag. Sizing: max(seasonal-bucket P95) + 25% safety margin so
	// the recommendation handles peak-of-day, not average-of-day.
	ClassDailyCyclical Class = "daily-cyclical"

	// ClassWeeklyCyclical — significant autocorrelation at the
	// 168-hour lag (weekly pattern). Sizing: same as daily-cyclical
	// but the seasonal bucket is the weekly cycle.
	ClassWeeklyCyclical Class = "weekly-cyclical"

	// ClassBursty — coefficient of variation > 1.0 with no clear
	// seasonality. Sizing: P99 + 50% safety margin because the tail
	// dominates and seasonal smoothing doesn't help.
	ClassBursty Class = "bursty"
)

// Sample is a single point in a workload's resource time-series. Time
// is required; Value is the observed metric (CPU millicores, memory
// bytes, or similar — the classifier treats the units opaquely).
type Sample struct {
	Time  time.Time
	Value float64
}

// Result is the classifier's output for one input series.
type Result struct {
	Class Class

	// CV is the coefficient of variation (stddev / mean) over the
	// input series. Reported for downstream confidence-band math.
	CV float64

	// Autocorr24h is the autocorrelation coefficient at the 24-hour
	// lag (range -1.0 to 1.0). Positive values above ~0.3 indicate
	// daily seasonality.
	Autocorr24h float64

	// Autocorr168h is the autocorrelation coefficient at the
	// 168-hour (weekly) lag.
	Autocorr168h float64

	// SampleCount is the number of input samples the classifier used
	// after windowing. Below the configured minimum the classifier
	// returns ErrInsufficientData rather than a Class.
	SampleCount int
}

// ErrInsufficientData is returned when the input series is too short
// to classify reliably. Callers should fall back to the safest sizing
// policy (steady-state with conservative margins) rather than guessing.
var ErrInsufficientData = errors.New("classify: insufficient data")

// Classifier statistically buckets a workload's observed time-series
// into one of the four behavior classes.
//
// The interface seam lets the orchestrator swap implementations: the
// production classifier (Phase 6) uses Box-Cox-transformed series and
// FFT-based autocorrelation; a Phase-2 sandbox classifier returns
// ClassSteady for any input so callers can be wired before the math
// lands.
type Classifier interface {
	// Classify takes a time-ordered usage series and returns its
	// behavior class plus the statistical evidence behind the
	// classification. Returns ErrInsufficientData if samples is
	// shorter than the implementation's minimum window.
	Classify(samples []Sample) (Result, error)
}

// SandboxClassifier is the Phase-2 stub: every input is classified as
// ClassSteady. It exists so the sizing engine can be wired and tested
// before the real statistical classifier lands in Phase 6. The
// production classifier is a drop-in replacement behind the same
// interface.
type SandboxClassifier struct{}

// Classify always returns ClassSteady. CV / autocorr fields are zero
// and SampleCount mirrors the input length. Returns ErrInsufficientData
// only if samples is empty.
func (SandboxClassifier) Classify(samples []Sample) (Result, error) {
	if len(samples) == 0 {
		return Result{}, ErrInsufficientData
	}
	return Result{
		Class:       ClassSteady,
		SampleCount: len(samples),
	}, nil
}
