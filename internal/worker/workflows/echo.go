// Package workflows holds the registered Optiqor workflows. Phase 1
// ships an Echo workflow that exercises the dispatcher end-to-end so
// the cmd/worker boot path is observable in tests and during local
// `make dev`. Real workflows (PR analysis, Apply Fix, Receipt
// issuance, Auto-Rollback monitor, Cost Spike) replace this in
// Phases 3-6.
package workflows

import (
	"context"
	"log/slog"

	"github.com/optiqor/backend/internal/tenancy"
	"github.com/optiqor/backend/internal/worker"
)

// EchoName is the registered workflow name; stable wire format.
const EchoName = "echo"

// Echo is a no-op workflow that logs the tenant scope and payload
// length. Useful for smoke-testing the dispatcher and for confirming
// the worker picked up a tenant-scoped queue submission.
type Echo struct {
	Logger *slog.Logger
}

// NewEcho returns an Echo workflow. The logger is injected so tests
// can assert on the structured-log output.
func NewEcho(logger *slog.Logger) *Echo {
	if logger == nil {
		logger = slog.Default()
	}
	return &Echo{Logger: logger}
}

// Name implements worker.Workflow.
func (*Echo) Name() string { return EchoName }

// Execute implements worker.Workflow.
func (e *Echo) Execute(ctx context.Context, t tenancy.Context, payload []byte) error {
	e.Logger.InfoContext(ctx, "echo workflow",
		"tenant_id", t.TenantID,
		"workspace_id", t.WorkspaceID,
		"payload_len", len(payload),
	)
	return nil
}

// Compile-time guard — drift here means the dispatcher contract
// changed and other workflows need updating.
var _ worker.Workflow = (*Echo)(nil)
