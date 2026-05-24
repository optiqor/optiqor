package confidence

import (
	"testing"

	"github.com/optiqor/optiqor-cli/pkg/rules"
)

func TestHeuristic_Classify(t *testing.T) {
	h := NewHeuristic()
	for _, tc := range []struct {
		name string
		in   Signal
		want rules.Confidence
	}{
		{
			name: "agent-7d-corroborated-high",
			in: Signal{
				SandboxMode:         false,
				HasHistoricalData:   true,
				HistoryDaysObserved: 7,
				SignalCount:         2,
				DetectorDefault:     rules.ConfidenceMed,
			},
			want: rules.ConfidenceHigh,
		},
		{
			name: "agent-7d-single-signal-floor-med",
			in: Signal{
				SandboxMode:         false,
				HasHistoricalData:   true,
				HistoryDaysObserved: 7,
				SignalCount:         1,
				DetectorDefault:     rules.ConfidenceMed,
			},
			want: rules.ConfidenceMed,
		},
		{
			name: "agent-under-7d-medium",
			in: Signal{
				SandboxMode:         false,
				HasHistoricalData:   true,
				HistoryDaysObserved: 3,
				SignalCount:         5,
				DetectorDefault:     rules.ConfidenceLow,
			},
			want: rules.ConfidenceMed,
		},
		{
			name: "sandbox-capped-at-medium",
			in: Signal{
				SandboxMode:     true,
				SignalCount:     5,
				DetectorDefault: rules.ConfidenceLow,
			},
			want: rules.ConfidenceMed,
		},
		{
			name: "sandbox-single-signal-low",
			in: Signal{
				SandboxMode:     true,
				SignalCount:     1,
				DetectorDefault: rules.ConfidenceLow,
			},
			want: rules.ConfidenceLow,
		},
		{
			name: "prior-dismissal-high-to-med",
			in: Signal{
				DetectorDefault: rules.ConfidenceHigh,
				PriorDismissed:  true,
			},
			want: rules.ConfidenceMed,
		},
		{
			name: "prior-dismissal-med-to-low",
			in: Signal{
				DetectorDefault: rules.ConfidenceMed,
				PriorDismissed:  true,
			},
			want: rules.ConfidenceLow,
		},
		{
			name: "prior-dismissal-low-stays-low",
			in: Signal{
				DetectorDefault: rules.ConfidenceLow,
				PriorDismissed:  true,
			},
			want: rules.ConfidenceLow,
		},
		{
			name: "empty-default-falls-to-low",
			in:   Signal{SandboxMode: true},
			want: rules.ConfidenceLow,
		},
		{
			name: "detector-default-high-agent-preserved",
			in: Signal{
				SandboxMode:         false,
				HasHistoricalData:   true,
				HistoryDaysObserved: 30,
				SignalCount:         3,
				DetectorDefault:     rules.ConfidenceHigh,
			},
			want: rules.ConfidenceHigh,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := h.Classify(tc.in); got != tc.want {
				t.Errorf("Classify = %q, want %q", got, tc.want)
			}
		})
	}
}

// Confidence is part of every PR comment + Receipt; non-determinism here
// would corrupt cryptographic verification. Pin it.
func TestHeuristic_DeterministicAcrossRuns(t *testing.T) {
	h := NewHeuristic()
	s := Signal{SandboxMode: true, SignalCount: 2, DetectorDefault: rules.ConfidenceLow}
	first := h.Classify(s)
	for i := 0; i < 100; i++ {
		if got := h.Classify(s); got != first {
			t.Fatalf("Classify is non-deterministic: %q vs %q at iter %d", got, first, i)
		}
	}
}
