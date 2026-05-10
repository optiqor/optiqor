package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/optiqor/backend/internal/rollback"
	"github.com/optiqor/backend/internal/tenancy"
)

// RollbackInitiator is the seam between the watchdog and the GitHub
// rollback-PR opener. Production uses [PRPublisher]; the workflow
// keeps a narrow interface so tests don't need a full pull-request
// fixture.
type RollbackInitiator interface {
	OpenRollback(ctx context.Context, t tenancy.Context, applyFixID string, reason string) error
}

// RollbackWatchdogPayload is the dispatcher input for one poll cycle.
type RollbackWatchdogPayload struct {
	State    rollback.State    `json:"state"`
	Snapshot rollback.Snapshot `json:"snapshot"`
	Now      time.Time         `json:"now"`
}

// RollbackWatchdog runs one watchdog step.
type RollbackWatchdog struct {
	Initiator RollbackInitiator
}

// Name is the dispatcher key.
func (RollbackWatchdog) Name() string { return "rollback_watchdog" }

// Execute decides whether the merged change crossed a bound. If so,
// it opens a rollback PR via the initiator. The workflow does NOT
// itself drive the polling cadence — Temporal schedules re-submission.
func (w RollbackWatchdog) Execute(ctx context.Context, t tenancy.Context, raw []byte) error {
	if w.Initiator == nil {
		return fmt.Errorf("rollback_watchdog: nil initiator")
	}
	var p RollbackWatchdogPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("rollback_watchdog: decode: %w", err)
	}
	now := p.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	d, err := rollback.Decide(p.State, p.Snapshot, now)
	if err != nil {
		return fmt.Errorf("rollback_watchdog: decide: %w", err)
	}
	if d.Action == rollback.ActionRollback {
		return w.Initiator.OpenRollback(ctx, t, p.State.ApplyFixID, d.Reason)
	}
	return nil
}
