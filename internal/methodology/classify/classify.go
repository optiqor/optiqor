package classify

import (
	"errors"
	"time"
)

// Class is the behaviour bucket the sizing engine consults to pick a
// percentile target and safety margin. See ADR-0009 and the spec at
// optiqor.dev/methodology/hybrid-v1.
type Class string

// Sizing policies per class:
//
//	steady           — CV < 0.2,            P95 + 30%
//	daily-cyclical   — 24h autocorr,        seasonal-max P95 + 25%
//	weekly-cyclical  — 168h autocorr,       seasonal-max P95 + 25%
//	bursty           — CV > 1.0, no season, P99 + 50%
const (
	ClassSteady         Class = "steady"
	ClassDailyCyclical  Class = "daily-cyclical"
	ClassWeeklyCyclical Class = "weekly-cyclical"
	ClassBursty         Class = "bursty"
)

// Sample is one point in a workload's time-series. Units are opaque
// (CPU millicores, memory bytes, etc).
type Sample struct {
	Time  time.Time
	Value float64
}

type Result struct {
	Class        Class
	CV           float64
	Autocorr24h  float64
	Autocorr168h float64
	SampleCount  int
}

// ErrInsufficientData signals the input is too short to classify
// reliably. Callers fall back to the conservative steady policy rather
// than guessing.
var ErrInsufficientData = errors.New("classify: insufficient data")

// Classifier is the seam. Production (Phase 6) uses Box-Cox + FFT
// autocorrelation; the Phase-2 SandboxClassifier returns ClassSteady
// so callers can wire before the math lands.
type Classifier interface {
	Classify(samples []Sample) (Result, error)
}

// SandboxClassifier returns ClassSteady for any non-empty input.
type SandboxClassifier struct{}

func (SandboxClassifier) Classify(samples []Sample) (Result, error) {
	if len(samples) == 0 {
		return Result{}, ErrInsufficientData
	}
	return Result{
		Class:       ClassSteady,
		SampleCount: len(samples),
	}, nil
}
