// Package workflows holds the workflows registered with
// worker.Dispatcher. Each is independently testable via the in-memory
// dispatcher.
package workflows

import (
	"context"
	"log/slog"

	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/worker"
)

// EchoName is the registered workflow name; stable wire format.
const EchoName = "echo"

// Echo logs the tenant scope and payload length. Smoke-test for the
// dispatcher and the cmd/worker boot path.
type Echo struct {
	Logger *slog.Logger
}

func NewEcho(logger *slog.Logger) *Echo {
	if logger == nil {
		logger = slog.Default()
	}
	return &Echo{Logger: logger}
}

func (*Echo) Name() string { return EchoName }

func (e *Echo) Execute(ctx context.Context, t tenancy.Context, payload []byte) error {
	e.Logger.InfoContext(ctx, "echo workflow",
		"tenant_id", t.TenantID,
		"workspace_id", t.WorkspaceID,
		"payload_len", len(payload),
	)
	return nil
}

var _ worker.Workflow = (*Echo)(nil)
