// Dev / Phase-1 dependency wiring for the worker's registered
// workflows. The same "noop until Phase-N lands" pattern as
// cmd/api/routes.go: production replaces each binding with a real
// implementation behind the same interface; the workflow code stays
// the same.
package main

import (
	"context"
	"log/slog"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/receipts"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/worker/workflows"
)

// noopLLM matches cmd/api/routes.go's stand-in: returns a canned
// "EXPLANATION:/DIFF:" pair so the Apply Fix workflow can render an
// end-to-end markdown body without an Anthropic API key.
type noopLLM struct{}

func (noopLLM) Generate(_ context.Context, _ agent.LLMRequest) (agent.LLMResponse, error) {
	return agent.LLMResponse{
		Text:  "EXPLANATION:\nLLM not configured in this environment.\nDIFF:\n",
		Model: "noop",
	}, nil
}

// loggingPRPublisher / loggingNotifier / loggingInitiator: dev-side
// implementations that log what *would* have happened. Production
// swaps them for the real GitHub / Slack / GitHub-rollback wiring.

type loggingPRPublisher struct{ log *slog.Logger }

func (p *loggingPRPublisher) Publish(_ context.Context, t tenancy.Context, pr workflows.PullRequest) (workflows.PRResult, error) {
	p.log.Info("would open PR",
		"tenant", t.TenantID, "repo", pr.RepoOwner+"/"+pr.RepoName,
		"head", pr.HeadBranch, "apply_fix_id", pr.ApplyFixID)
	return workflows.PRResult{URL: "https://example/local-dev/pr/0", Number: 0}, nil
}

type loggingRollbackInitiator struct{ log *slog.Logger }

func (i *loggingRollbackInitiator) OpenRollback(_ context.Context, t tenancy.Context, applyFixID, reason string) error {
	i.log.Warn("would open rollback PR",
		"tenant", t.TenantID, "apply_fix_id", applyFixID, "reason", reason)
	return nil
}

type loggingSpikeNotifier struct{ log *slog.Logger }

func (n *loggingSpikeNotifier) NotifySpike(_ context.Context, t tenancy.Context, e workflows.SpikeEvent) error {
	n.log.Warn("cost spike detected",
		"tenant", t.TenantID, "workload", e.WorkloadID,
		"delta_usd", e.ObservedDeltaUSD, "likely_pr", e.LikelyPRURL)
	return nil
}

// inMemoryReceiptStore stores issued receipts in-process so the
// Phase-1 worker can exercise the issuance path end-to-end without a
// Postgres dependency. Production swaps for the receipts table.
type inMemoryReceiptStore struct {
	log *slog.Logger
	m   map[string]struct {
		signed  string
		receipt receipts.Receipt
	}
}

func newInMemoryReceiptStore(log *slog.Logger) *inMemoryReceiptStore {
	return &inMemoryReceiptStore{
		log: log,
		m: map[string]struct {
			signed  string
			receipt receipts.Receipt
		}{},
	}
}

func (s *inMemoryReceiptStore) Save(_ context.Context, t tenancy.Context, id, signed string, r receipts.Receipt) error {
	s.m[id] = struct {
		signed  string
		receipt receipts.Receipt
	}{signed, r}
	s.log.Info("receipt issued",
		"tenant", t.TenantID, "receipt_id", id,
		"workload", r.Workload,
		"realised_usd_cents", r.RealisedSavingsUSDCents)
	return nil
}
