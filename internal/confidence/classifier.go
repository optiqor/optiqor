// Package confidence classifies a finding's confidence band — the
// "low / medium / high" signal that ships next to every recommendation
// so users know whether to act now or run an experiment first.
//
// Phase 1 is a deterministic rule-based classifier; Phase 2 swaps in a
// trained model behind the same [Classifier] interface. The detector
// library can already attach a default band, but the classifier in
// this package consumes the wider signal set the backend has (sandbox
// vs agent mode, observed P95 from Prometheus, prior dismissals) and
// can override the detector's band when context warrants it.
//
// Decision rules (Phase 1):
//
//	high      — agent-mode evidence with ≥7 days of metrics AND no prior
//	            dismissal AND signal_count ≥ 2 (e.g. P95 + P99 agree).
//	medium    — agent-mode with <7 days OR sandbox-mode with strong
//	            static evidence (e.g. explicit `:latest` tag).
//	low       — sandbox-mode with single-signal default; everything
//	            else that lacks measured data.
//
// Each branch is exercised by a table-driven unit test so changes are
// reviewable without re-running the whole platform.
package confidence

import (
	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// Signal describes the evidence backing a finding at decision time.
// Build one per [rules.Finding] before calling [Classify].
type Signal struct {
	DetectorID         string
	SandboxMode        bool // true → CLI/sandbox path (no Prometheus); false → agent
	HasHistoricalData  bool // ≥1 day of Prometheus history
	HistoryDaysObserved int  // exact count; used to disambiguate medium ↔ high
	SignalCount        int  // independent corroborations (e.g. P95+P99+request)
	PriorDismissed     bool // tenant previously dismissed the same detector on this workload
	DetectorDefault    rules.Confidence
}

// Classifier returns a [rules.Confidence] band for the input signal.
// The interface exists so the Phase 2 model can replace the heuristic
// at runtime without rippling through callers.
type Classifier interface {
	Classify(Signal) rules.Confidence
}

// Heuristic is the Phase 1 deterministic classifier. Stateless; share
// one instance across all goroutines.
type Heuristic struct{}

// NewHeuristic returns the default Phase 1 classifier.
func NewHeuristic() Heuristic { return Heuristic{} }

// MinDaysForHighBand is the agent-mode threshold above which a
// well-corroborated finding is allowed to escalate to "high".
const MinDaysForHighBand = 7

// MinSignalsForHighBand is the corroboration threshold (e.g. P95 + P99
// of the same metric agreeing) above which we will issue "high".
const MinSignalsForHighBand = 2

// Classify applies the Phase 1 rule set. Behaviour:
//
//   - A prior dismissal never escalates a band — it only de-escalates.
//   - Sandbox mode caps at medium; only an agent has the measured
//     data to support "high".
//   - The detector's own default is the floor: we never *lower* a
//     detector that already says high.
func (h Heuristic) Classify(s Signal) rules.Confidence {
	out := s.DetectorDefault
	if out == "" {
		out = rules.ConfidenceLow
	}

	if s.PriorDismissed {
		// Customer told us this rule is noisy on this workload. Drop
		// one band so the next surface is less aggressive without
		// removing the finding entirely.
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
		// keep the detector's default, but cap sandbox single-signal
		// findings at low — they're directional, not bankable.
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
