// Package rollback owns the Auto-Rollback Guarantee decision logic:
// given a metric snapshot, does the watchdog roll back the Apply Fix?
// Snapshot collection lives in [ingestion]; PR opening lives in
// prwriter / cmd/api. Split this way so the state machine stays pure
// and table-testable.
package rollback

import (
	"errors"
	"time"
)

// Window is how long the watchdog observes a merged change, per the
// Auto-Rollback Guarantee spec.
const Window = 7 * 24 * time.Hour

// SignalKind is the metric being observed. New kinds must extend both
// this constant block and the Decide branch in lockstep.
type SignalKind string

const (
	SignalLatencyP95    SignalKind = "latency_p95_ms"
	SignalErrorRate     SignalKind = "error_rate_bps"
	SignalCPUSaturation SignalKind = "cpu_throttle_ratio"
)

type Snapshot struct {
	Kind       SignalKind
	Value      float64
	ObservedAt time.Time
}

type Bound struct {
	Kind     SignalKind
	RealMax  float64 // hard ceiling; crossing it triggers rollback
	Baseline float64 // pre-change baseline, surfaced in the rollback PR body
}

type Decision struct {
	Action  Action
	Reason  string
	Trigger Snapshot // snapshot that crossed the bound; zero when Action != Rollback
}

type Action string

const (
	ActionContinue Action = "continue"
	ActionRollback Action = "rollback"
	ActionClose    Action = "close"
)

// State is persisted between snapshot polls (Postgres or Temporal
// workflow state).
type State struct {
	ApplyFixID string
	MergedAt   time.Time
	Bounds     []Bound
	Closed     bool
}

func (s State) Validate() error {
	switch {
	case s.ApplyFixID == "":
		return errors.New("rollback: missing apply_fix_id")
	case s.MergedAt.IsZero():
		return errors.New("rollback: missing merged_at")
	case len(s.Bounds) == 0:
		return errors.New("rollback: at least one bound required")
	}
	return nil
}

// Decide evaluates a snapshot against the state's bounds. Invariants
// tests pin:
//   - past now-Window closes the watch;
//   - unbounded snapshot Kinds are ignored (Continue);
//   - first bound crossing triggers Rollback;
//   - a Closed state is terminal.
func Decide(s State, snap Snapshot, now time.Time) (Decision, error) {
	if err := s.Validate(); err != nil {
		return Decision{}, err
	}
	if s.Closed {
		return Decision{Action: ActionClose, Reason: "watchdog already closed"}, nil
	}
	if !now.Before(s.MergedAt.Add(Window)) {
		return Decision{Action: ActionClose, Reason: "watchdog window expired"}, nil
	}
	for _, b := range s.Bounds {
		if b.Kind != snap.Kind {
			continue
		}
		if snap.Value > b.RealMax {
			return Decision{
				Action:  ActionRollback,
				Reason:  "metric crossed the predicted upper bound",
				Trigger: snap,
			}, nil
		}
	}
	return Decision{Action: ActionContinue, Reason: "within bounds"}, nil
}

// Close marks the state finalised. Idempotent.
func Close(s State) State {
	s.Closed = true
	return s
}

func Elapsed(s State, now time.Time) time.Duration {
	return now.Sub(s.MergedAt)
}
