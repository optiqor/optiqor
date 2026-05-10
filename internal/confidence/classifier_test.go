package confidence

import (
	"testing"

	"github.com/optiqor/optiqor-cli/pkg/rules"
)

func TestHeuristic_TableDriven(t *testing.T) {
	type tc struct {
		name string
		in   Signal
		want rules.Confidence
	}
	cases := []tc{
		{
			name: "agent + 7d + corroborated → high",
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
			name: "agent + 7d but single-signal stays at floor=med",
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
			name: "agent + <7d → medium",
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
			name: "sandbox capped at medium even with strong corroboration",
			in: Signal{
				SandboxMode:     true,
				SignalCount:     5,
				DetectorDefault: rules.ConfidenceLow,
			},
			want: rules.ConfidenceMed,
		},
		{
			name: "sandbox single-signal stays low",
			in: Signal{
				SandboxMode:     true,
				SignalCount:     1,
				DetectorDefault: rules.ConfidenceLow,
			},
			want: rules.ConfidenceLow,
		},
		{
			name: "prior dismissal de-escalates from high to medium",
			in: Signal{
				DetectorDefault: rules.ConfidenceHigh,
				PriorDismissed:  true,
			},
			want: rules.ConfidenceMed,
		},
		{
			name: "prior dismissal de-escalates from medium to low",
			in: Signal{
				DetectorDefault: rules.ConfidenceMed,
				PriorDismissed:  true,
			},
			want: rules.ConfidenceLow,
		},
		{
			name: "prior dismissal at low stays at low",
			in: Signal{
				DetectorDefault: rules.ConfidenceLow,
				PriorDismissed:  true,
			},
			want: rules.ConfidenceLow,
		},
		{
			name: "empty default falls through to low",
			in:   Signal{SandboxMode: true},
			want: rules.ConfidenceLow,
		},
		{
			name: "detector default high in agent mode preserved",
			in: Signal{
				SandboxMode:         false,
				HasHistoricalData:   true,
				HistoryDaysObserved: 30,
				SignalCount:         3,
				DetectorDefault:     rules.ConfidenceHigh,
			},
			want: rules.ConfidenceHigh,
		},
	}
	h := NewHeuristic()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := h.Classify(c.in); got != c.want {
				t.Errorf("Classify = %q, want %q", got, c.want)
			}
		})
	}
}

func TestHeuristic_DeterministicAcrossRuns(t *testing.T) {
	// Confidence is part of every PR comment + Receipt; non-determinism
	// here would corrupt cryptographic verification. Pin it.
	h := NewHeuristic()
	s := Signal{SandboxMode: true, SignalCount: 2, DetectorDefault: rules.ConfidenceLow}
	first := h.Classify(s)
	for i := 0; i < 100; i++ {
		if got := h.Classify(s); got != first {
			t.Fatalf("Classify is non-deterministic: %q vs %q at iter %d", got, first, i)
		}
	}
}
