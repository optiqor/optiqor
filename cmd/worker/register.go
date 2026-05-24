package main

import (
	"log/slog"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/applyfix/gate"
	"github.com/optiqor/optiqor/internal/receipts"
	"github.com/optiqor/optiqor/internal/worker"
	"github.com/optiqor/optiqor/internal/worker/workflows"
)

// registerWorkflows binds every Year-1 workflow with Phase-1 dev
// deps. A registration failure aborts the whole binding so we never
// boot with a partial surface.
func registerWorkflows(d *worker.InMemory, log *slog.Logger) error {
	composer := &agent.Composer{
		LLM:    noopLLM{},
		Budget: agent.Budget{PerCallCents: 40},
	}

	// Ephemeral Ed25519 signer; restart rotates the key. Production
	// loads from KMS.
	issuer, _, err := receipts.GenerateIssuer("worker-dev")
	if err != nil {
		return err
	}

	// render + conform + post are real Phase-4 stages; dryrun still
	// stub (Phase-5 agent round-trip). Flip to StrictPolicy once dryrun
	// lands.
	gatePipeline := gate.NewPipeline(gate.SkeletonPolicy{},
		gate.RenderValidator{},
		gate.ConformValidator{},
		gate.NotImplementedValidator{S: gate.StageDryrun},
		gate.PostValidator{MaxResourceReductionRatio: 0.5},
	)

	bound := []worker.Workflow{
		workflows.NewEcho(log),
		workflows.ApplyFix{
			Composer:  composer,
			Gate:      gatePipeline,
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
