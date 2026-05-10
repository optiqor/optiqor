// Package rollback implements the Auto-Rollback Guarantee:
//
//	After an Apply Fix PR merges, we keep watching the workload's
//	live metrics for 7 days. If the observed signal drifts outside
//	the bound predicted in the Receipt, we open an automated
//	rollback PR.
//
// This package owns the *decision* logic — given a series of metric
// snapshots, does the watchdog decide to roll back? The actual
// snapshot collection lives in [ingestion]; the rollback-PR opening
// lives in [prwriter]/cmd/api. Splitting it this way keeps the
// state-machine deterministic and table-testable.
package rollback

import (
	"errors"
	"time"
)

// Window is how long the watchdog observes a merged change. 7 days
// per the Auto-Rollback Guarantee spec.
const Window = 7 * 24 * time.Hour

// SignalKind is the metric being observed. Phase 1 watches the same
// three signals Verified Receipts rely on; new kinds extend the union
// type and the Decide branch in lockstep.
type SignalKind string

const (
	// SignalLatencyP95 watches request-latency P95 in milliseconds.
	SignalLatencyP95 SignalKind = "latency_p95_ms"
	// SignalErrorRate watches the request error rate, in basis points.
	SignalErrorRate SignalKind = "error_rate_bps"
	// SignalCPUSaturation watches the workload's CPU throttle ratio.
	SignalCPUSaturation SignalKind = "cpu_throttle_ratio"
)

// SignalSnapshot is one Prometheus query result. Watchdog logic is
// pure functions over a sequence of these.
type Snapshot struct {
	Kind      SignalKind
	Value     float64
	ObservedAt time.Time
}

// Predicted bound the Apply Fix promised. RealMax is the worst the
// watchdog will tolerate before rolling back. The Receipt issued
// after the change confirms whether reality stayed below RealMax.
type Bound struct {
	Kind     SignalKind
	RealMax  float64 // hard ceiling; cross this and we roll back
	Baseline float64 // pre-change baseline, for the rollback PR body
}

// Decision is what Decide returns. Action is the watchdog's verdict;
// Reason is the audit string surfaced in the Receipt and rollback
// PR.
type Decision struct {
	Action  Action
	Reason  string
	Trigger Snapshot // the snapshot that crossed the bound (zero if none)
}

// Action is the watchdog's verdict.
type Action string

const (
	// ActionContinue means keep observing.
	ActionContinue Action = "continue"
	// ActionRollback means open a rollback PR.
	ActionRollback Action = "rollback"
	// ActionClose means the window is over; the change is final.
	ActionClose Action = "close"
)

// State carries the merged-PR identity through the watchdog. The
// caller persists this between snapshot polls (Postgres or
// Temporal-workflow state).
type State struct {
	ApplyFixID string
	MergedAt   time.Time
	Bounds     []Bound
	Closed     bool
}

// Validate returns an error for an unusable state — empty fix id,
// zero merge timestamp, or no bounds at all.
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

// Decide evaluates a new snapshot against the state's bounds and
// returns the watchdog's action. Properties:
//
//   - Out-of-window snapshots (older than now-Window) close the watch.
//   - Snapshots whose Kind has no bound are ignored (Continue).
//   - The first snapshot crossing any bound triggers Rollback.
//   - A closed state always returns Close (idempotent).
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

// Elapsed reports the duration the watchdog has been running.
func Elapsed(s State, now time.Time) time.Duration {
	return now.Sub(s.MergedAt)
}
