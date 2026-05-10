package main

import (
	"log/slog"

	"github.com/optiqor/backend/internal/agent"
	"github.com/optiqor/backend/internal/receipts"
	"github.com/optiqor/backend/internal/worker"
	"github.com/optiqor/backend/internal/worker/workflows"
)

// registerWorkflows binds every Year-1 workflow to the dispatcher
// with Phase-1 dev dependencies (logging publisher, in-memory
// receipt store, fresh ephemeral signing key). Production cmd/worker
// wires the real adapters; the dispatcher API stays the same.
//
// Failing to register any one workflow returns immediately — partial
// registration would silently leak request-handling for the missing
// surface.
func registerWorkflows(d *worker.InMemory, log *slog.Logger) error {
	composer := &agent.Composer{
		LLM:    noopLLM{},
		Budget: agent.Budget{PerCallCents: 40},
	}

	// Ephemeral Ed25519 signer for dev. Restarting the worker rotates
	// the key — fine for local; production loads from KMS.
	issuer, _, err := receipts.GenerateIssuer("worker-dev")
	if err != nil {
		return err
	}

	bound := []worker.Workflow{
		workflows.NewEcho(log),
		workflows.ApplyFix{
			Composer:  composer,
			Publisher: &loggingPRPublisher{log: log},
		},
		workflows.ReceiptIssue{
			Issuer: issuer,
			Store:  newInMemoryReceiptStore(log),
		},
		workflows.RollbackWatchdog{
			Initiator: &loggingRollbackInitiator{log: log},
		},
		workflows.CostSpike{
			Notifier: &loggingSpikeNotifier{log: log},
		},
	}
	for _, w := range bound {
		if err := d.Register(w); err != nil {
			return err
		}
	}
	return nil
}
