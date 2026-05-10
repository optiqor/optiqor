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

func TestDecide_HappyPath_ContinueWithinBounds(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	s := mkState(now)
	d, err := Decide(s, Snapshot{Kind: SignalLatencyP95, Value: 200, ObservedAt: now.Add(1 * time.Hour)}, now.Add(1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != ActionContinue {
		t.Errorf("action = %q, want continue", d.Action)
	}
}

func TestDecide_TriggersRollbackWhenBoundCrossed(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	s := mkState(now)
	trigger := Snapshot{Kind: SignalLatencyP95, Value: 400, ObservedAt: now.Add(1 * time.Hour)}
	d, err := Decide(s, trigger, now.Add(1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != ActionRollback {
		t.Errorf("action = %q, want rollback", d.Action)
	}
	if d.Trigger.Value != 400 {
		t.Errorf("trigger snapshot lost: %+v", d.Trigger)
	}
}

func TestDecide_UnboundedKindIgnored(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	s := mkState(now)
	d, _ := Decide(s, Snapshot{Kind: SignalCPUSaturation, Value: 9.99, ObservedAt: now.Add(1 * time.Hour)}, now.Add(1*time.Hour))
	if d.Action != ActionContinue {
		t.Errorf("snapshot of unbounded kind should be ignored")
	}
}

func TestDecide_WindowExpires(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	s := mkState(now)
	d, _ := Decide(s, Snapshot{Kind: SignalLatencyP95, Value: 200}, now.Add(Window+time.Hour))
	if d.Action != ActionClose {
		t.Errorf("expired window should close, got %q", d.Action)
	}
}

func TestDecide_ClosedStateIsTerminal(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	s := Close(mkState(now))
	d, _ := Decide(s, Snapshot{Kind: SignalLatencyP95, Value: 9999}, now.Add(1*time.Hour))
	if d.Action != ActionClose {
		t.Errorf("closed state should stay closed even with violations")
	}
}

func TestState_Validate(t *testing.T) {
	if err := (State{}).Validate(); err == nil {
		t.Error("empty state should fail validation")
	}
	now := time.Now()
	s := mkState(now)
	if err := s.Validate(); err != nil {
		t.Errorf("valid state failed: %v", err)
	}
}

func TestDecide_DeterministicAcrossRuns(t *testing.T) {
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

func TestElapsed_Linear(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	s := mkState(now)
	if got := Elapsed(s, now.Add(2*time.Hour)); got != 2*time.Hour {
		t.Errorf("Elapsed = %v", got)
	}
}
