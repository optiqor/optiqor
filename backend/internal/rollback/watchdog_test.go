package rollback

import (
	"testing"
	"time"
)

func mkState(now time.Time) State {
	return State{
		ApplyFixID: "afix_1",
		MergedAt:   now,
		Bounds: []Bound{
			{Kind: SignalLatencyP95, RealMax: 250, Baseline: 180},
			{Kind: SignalErrorRate, RealMax: 100, Baseline: 30},
		},
	}
}

func TestDecide(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		closed      bool
		snap        Snapshot
		evalAt      time.Time
		wantAction  Action
		wantTrigger float64 // 0 when not asserting
	}{
		{
			name:       "value within bound continues",
			snap:       Snapshot{Kind: SignalLatencyP95, Value: 200, ObservedAt: now.Add(time.Hour)},
			evalAt:     now.Add(time.Hour),
			wantAction: ActionContinue,
		},
		{
			name:        "value over bound triggers rollback and preserves snapshot",
			snap:        Snapshot{Kind: SignalLatencyP95, Value: 400, ObservedAt: now.Add(time.Hour)},
			evalAt:      now.Add(time.Hour),
			wantAction:  ActionRollback,
			wantTrigger: 400,
		},
		{
			name:       "unbounded signal kind is ignored",
			snap:       Snapshot{Kind: SignalCPUSaturation, Value: 9.99, ObservedAt: now.Add(time.Hour)},
			evalAt:     now.Add(time.Hour),
			wantAction: ActionContinue,
		},
		{
			name:       "evaluation past window closes",
			snap:       Snapshot{Kind: SignalLatencyP95, Value: 200},
			evalAt:     now.Add(Window + time.Hour),
			wantAction: ActionClose,
		},
		{
			name:       "closed state ignores subsequent violations",
			closed:     true,
			snap:       Snapshot{Kind: SignalLatencyP95, Value: 9999},
			evalAt:     now.Add(time.Hour),
			wantAction: ActionClose,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mkState(now)
			if tc.closed {
				s = Close(s)
			}
			d, err := Decide(s, tc.snap, tc.evalAt)
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if d.Action != tc.wantAction {
				t.Errorf("action = %q, want %q", d.Action, tc.wantAction)
			}
			if tc.wantTrigger != 0 && d.Trigger.Value != tc.wantTrigger {
				t.Errorf("trigger snapshot lost: %+v", d.Trigger)
			}
		})
	}
}

func TestDecide_DeterministicAcrossRuns(t *testing.T) {
	// Same (state, snapshot, now) MUST produce byte-identical Decision —
	// the Auto-Rollback Guarantee depends on this for replayable audit.
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	s := mkState(now)
	snap := Snapshot{Kind: SignalErrorRate, Value: 50, ObservedAt: now}
	first, _ := Decide(s, snap, now.Add(time.Hour))
	for i := 0; i < 50; i++ {
		got, _ := Decide(s, snap, now.Add(time.Hour))
		if got != first {
			t.Fatalf("non-deterministic Decide at iter %d: %+v vs %+v", i, got, first)
		}
	}
}

func TestState_Validate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   State
		wantErr bool
	}{
		{name: "zero value fails", state: State{}, wantErr: true},
		{name: "fully populated passes", state: mkState(time.Now()), wantErr: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.state.Validate()
			if tc.wantErr && err == nil {
				t.Error("want validation error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestElapsed_Linear(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	s := mkState(now)
	if got := Elapsed(s, now.Add(2*time.Hour)); got != 2*time.Hour {
		t.Errorf("Elapsed = %v", got)
	}
}
