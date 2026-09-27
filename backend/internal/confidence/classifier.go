// Package confidence classifies a finding's confidence band. Phase 1
// is the deterministic Heuristic below; Phase 2 will swap a trained
// model in behind the same [Classifier] interface.
package confidence

import (
	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// Signal describes the evidence backing a finding at decision time.
// Build one per [rules.Finding] before calling [Classify].
type Signal struct {
	DetectorID          string
	SandboxMode         bool
	HasHistoricalData   bool
	HistoryDaysObserved int
	SignalCount         int
	PriorDismissed      bool
	DetectorDefault     rules.Confidence
}

// Classifier is the seam Phase 2 will replace; keep callers off the
// concrete Heuristic.
type Classifier interface {
	Classify(Signal) rules.Confidence
}

// Heuristic is stateless; share one instance across goroutines.
type Heuristic struct{}

func NewHeuristic() Heuristic { return Heuristic{} }

const MinDaysForHighBand = 7
const MinSignalsForHighBand = 2

// Classify applies the Phase 1 rule set. Invariants:
//   - PriorDismissed only de-escalates; it never raises a band.
//   - Sandbox mode caps at medium because only an agent has the measured
//     data to support "high".
//   - DetectorDefault is the floor; we never lower a detector that
//     already says high.
func (h Heuristic) Classify(s Signal) rules.Confidence {
	out := s.DetectorDefault
	if out == "" {
		out = rules.ConfidenceLow
	}

	if s.PriorDismissed {
		out = stepDown(out)
		return out
	}

	switch {
	case !s.SandboxMode && s.HasHistoricalData &&
		s.HistoryDaysObserved >= MinDaysForHighBand &&
		s.SignalCount >= MinSignalsForHighBand:
		out = stepUpToHigh(out)
	case !s.SandboxMode && s.HasHistoricalData:
		out = atLeast(out, rules.ConfidenceMed)
	case s.SandboxMode && s.SignalCount >= MinSignalsForHighBand:
		out = atLeast(out, rules.ConfidenceMed)
	default:
		// Sandbox single-signal findings are directional, not bankable.
		if s.SandboxMode {
			out = atMost(out, rules.ConfidenceMed)
		}
	}
	return out
}

func stepDown(c rules.Confidence) rules.Confidence {
	switch c {
	case rules.ConfidenceHigh:
		return rules.ConfidenceMed
	case rules.ConfidenceMed:
		return rules.ConfidenceLow
	}
	return rules.ConfidenceLow
}

func stepUpToHigh(c rules.Confidence) rules.Confidence {
	if c == rules.ConfidenceHigh {
		return c
	}
	return rules.ConfidenceHigh
}

func atLeast(have, floor rules.Confidence) rules.Confidence {
	if rank(have) >= rank(floor) {
		return have
	}
	return floor
}

func atMost(have, ceiling rules.Confidence) rules.Confidence {
	if rank(have) <= rank(ceiling) {
		return have
	}
	return ceiling
}

func rank(c rules.Confidence) int {
	switch c {
	case rules.ConfidenceHigh:
		return 3
	case rules.ConfidenceMed:
		return 2
	case rules.ConfidenceLow:
		return 1
	}
	return 0
}
