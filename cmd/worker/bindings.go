// Phase-1 dev dependency wiring for the worker. Production replaces
// each binding behind the same interface; the workflow code does not
// change. Mirrors the noop pattern in cmd/api/routes.go.
package main

import (
	"context"
	"log/slog"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/receipts"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/worker/workflows"
)

// noopLLM mirrors cmd/api/routes.go's stand-in: returns a canned
// EXPLANATION/DIFF pair so Apply Fix renders end-to-end without an
// Anthropic API key.
type noopLLM struct{}

func (noopLLM) Generate(_ context.Context, _ agent.LLMRequest) (agent.LLMResponse, error) {
	return agent.LLMResponse{
		Text:  "EXPLANATION:\nLLM not configured in this environment.\nDIFF:\n",
		Model: "noop",
	}, nil
}

// loggingPRPublisher / loggingRollbackInitiator / loggingSpikeNotifier
// log what *would* have happened. Production swaps them for the real
// GitHub / Slack / rollback wiring.

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

// inMemoryReceiptStore lets Phase-1 exercise the issuance path
// end-to-end without Postgres. Production swaps for the receipts table.
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
