package main

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/applyfix/gate"
	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/receipts"
	"github.com/optiqor/optiqor/internal/worker"
	"github.com/optiqor/optiqor/internal/worker/workflows"
)

// registerWorkflows binds every Year-1 workflow. A registration failure
// aborts the whole binding so we never boot with a partial surface.
// pool may be nil in dev (in-memory recorder skip); cfg.AnthropicAPIKey
// flips the LLM from noop to the real adapter.
func registerWorkflows(d *worker.InMemory, log *slog.Logger, cfg config.Config, pool *pgxpool.Pool) error {
	composer := &agent.Composer{
		LLM:      pickLLM(cfg, log),
		Budget:   agent.Budget{PerCallCents: 40},
		Recorder: pickRecorder(pool),
	}

	// Ephemeral Ed25519 signer; restart rotates the key. Production
	// loads from KMS.
	issuer, _, err := receipts.GenerateIssuer("worker-dev")
	if err != nil {
		return err
	}

	// render + conform + post are real Phase-4 stages. The dryrun
	// validator carries no AgentClient until Phase-5 onboarding installs
	// the agent — it reports NotImplemented and SkeletonPolicy lets it
	// through. Flip to StrictPolicy + a real AgentClient at Phase-5 boot.
	gatePipeline := gate.NewPipeline(gate.SkeletonPolicy{},
		gate.RenderValidator{},
		gate.ConformValidator{},
		gate.DryrunValidator{},
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
