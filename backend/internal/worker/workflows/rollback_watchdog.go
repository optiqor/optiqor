package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/optiqor/optiqor/internal/rollback"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// RollbackInitiator is narrow on purpose so tests don't need a full
// pull-request fixture. Production wires PRPublisher behind it.
type RollbackInitiator interface {
	OpenRollback(ctx context.Context, t tenancy.Context, applyFixID string, reason string) error
}

type RollbackWatchdogPayload struct {
	State    rollback.State    `json:"state"`
	Snapshot rollback.Snapshot `json:"snapshot"`
	Now      time.Time         `json:"now"`
}

type RollbackWatchdog struct {
	Initiator RollbackInitiator
}

func (RollbackWatchdog) Name() string { return "rollback_watchdog" }

// Execute runs one poll cycle. Temporal drives the cadence by
// scheduling re-submission; the workflow itself is single-shot.
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
