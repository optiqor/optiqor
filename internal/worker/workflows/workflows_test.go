package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/receipts"
	"github.com/optiqor/optiqor/internal/rollback"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/worker"
)

// ---- Apply Fix -------------------------------------------------------

type fakePublisher struct {
	mu     sync.Mutex
	called []PullRequest
	err    error
}

func (f *fakePublisher) Publish(_ context.Context, _ tenancy.Context, p PullRequest) (PRResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = append(f.called, p)
	if f.err != nil {
		return PRResult{}, f.err
	}
	return PRResult{URL: "https://example/pr/1", Number: 1}, nil
}

func TestApplyFix_HappyPath_PublishesPR(t *testing.T) {
	llm := &agent.FakeLLMClient{Responses: []agent.LLMResponse{{
		Text:  "EXPLANATION:\nfix CPU\nDIFF:\n--- a\n+++ b\n@@\n- cpu: 2\n+ cpu: 1\n",
		Model: "claude-sonnet",
	}}}
	pub := &fakePublisher{}
	wf := ApplyFix{
		Composer:  &agent.Composer{LLM: llm},
		Publisher: pub,
	}
	disp := worker.NewInMemory()
	if err := disp.Register(wf); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(ApplyFixPayload{
		RepoOwner:  "acme",
		RepoName:   "api",
		BaseBranch: "main",
		ChartPath:  "charts/api",
		ChartYAML:  "api:\n  resources: {requests: {cpu: 2}}",
		Model:      "claude-sonnet",
		Finding: rules.Finding{
			DetectorID: "cpu-overprovisioned",
			Workload:   "api",
			Title:      "CPU overprovisioned",
			Severity:   rules.SeverityMed,
			Category:   rules.CategoryCost,
		},
		ApplyFixID: "afix_1",
		Now:        time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
	})
	err := disp.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, worker.QueueDefault, "apply_fix", payload)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(pub.called) != 1 {
		t.Fatalf("publisher called %d times", len(pub.called))
	}
	pr := pub.called[0]
	if pr.RepoOwner != "acme" || pr.RepoName != "api" {
		t.Errorf("pr coords: %+v", pr)
	}
	if pr.HeadBranch != "optiqor/apply-fix/afix_1" {
		t.Errorf("head branch = %q", pr.HeadBranch)
	}
	if pr.Body == "" {
		t.Error("PR body empty")
	}
}

func TestApplyFix_LLMFailure_Propagates(t *testing.T) {
	llm := &agent.FakeLLMClient{Err: errors.New("rate limit")}
	wf := ApplyFix{Composer: &agent.Composer{LLM: llm}, Publisher: &fakePublisher{}}
	payload, _ := json.Marshal(ApplyFixPayload{
		ChartYAML: "api: {}",
		Finding:   rules.Finding{Workload: "api", Title: "x"},
	})
	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload)
	if err == nil {
		t.Error("want error when LLM fails")
	}
}

func TestApplyFix_BadJSONRejected(t *testing.T) {
	wf := ApplyFix{Composer: &agent.Composer{LLM: &agent.FakeLLMClient{}}, Publisher: &fakePublisher{}}
	err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, []byte("not json"))
	if err == nil {
		t.Error("want error on malformed payload")
	}
}

// ---- Receipt Issue ---------------------------------------------------

type fakeReceiptStore struct {
	mu    sync.Mutex
	saved []struct {
		ID      string
		Signed  string
		Receipt receipts.Receipt
	}
}

func (s *fakeReceiptStore) Save(_ context.Context, _ tenancy.Context, id, signed string, r receipts.Receipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved = append(s.saved, struct {
		ID      string
		Signed  string
		Receipt receipts.Receipt
	}{id, signed, r})
	return nil
}

func TestReceiptIssue_SignsAndStores(t *testing.T) {
	iss, _, err := receipts.GenerateIssuer("k1")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeReceiptStore{}
	wf := ReceiptIssue{Issuer: iss, Store: store}
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	payload, _ := json.Marshal(ReceiptIssuePayload{
		Receipt: receipts.Receipt{
			ID:                       "rcpt_1",
			TenantID:                 "t1",
			Workload:                 "api",
			ApplyFixID:               "afix_1",
			ObservedFromUTC:          now.Add(-30 * 24 * time.Hour),
			ObservedToUTC:            now,
			PredictedSavingsUSDCents: 1000,
			RealisedSavingsUSDCents:  950,
			CloudBillSource:          "aws/cur",
		},
	})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatalf("saved %d, want 1", len(store.saved))
	}
	if store.saved[0].ID != "rcpt_1" {
		t.Errorf("id = %q", store.saved[0].ID)
	}
}

// ---- Rollback Watchdog ----------------------------------------------

type fakeInitiator struct {
	mu         sync.Mutex
	opened     []string
	openCalled int
}

func (f *fakeInitiator) OpenRollback(_ context.Context, _ tenancy.Context, applyFixID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opened = append(f.opened, applyFixID)
	f.openCalled++
	return nil
}

func TestRollbackWatchdog_TriggersWhenBoundCrossed(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	state := rollback.State{
		ApplyFixID: "afix_99",
		MergedAt:   now,
		Bounds:     []rollback.Bound{{Kind: rollback.SignalLatencyP95, RealMax: 100, Baseline: 50}},
	}
	init := &fakeInitiator{}
	wf := RollbackWatchdog{Initiator: init}
	payload, _ := json.Marshal(RollbackWatchdogPayload{
		State:    state,
		Snapshot: rollback.Snapshot{Kind: rollback.SignalLatencyP95, Value: 250, ObservedAt: now.Add(time.Hour)},
		Now:      now.Add(time.Hour),
	})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(init.opened) != 1 || init.opened[0] != "afix_99" {
		t.Errorf("initiator not called for the right fix: %+v", init.opened)
	}
}

func TestRollbackWatchdog_NoActionWhenWithinBounds(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	state := rollback.State{
		ApplyFixID: "afix_99",
		MergedAt:   now,
		Bounds:     []rollback.Bound{{Kind: rollback.SignalLatencyP95, RealMax: 1000}},
	}
	init := &fakeInitiator{}
	wf := RollbackWatchdog{Initiator: init}
	payload, _ := json.Marshal(RollbackWatchdogPayload{
		State:    state,
		Snapshot: rollback.Snapshot{Kind: rollback.SignalLatencyP95, Value: 100, ObservedAt: now},
		Now:      now.Add(time.Hour),
	})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload); err != nil {
		t.Fatal(err)
	}
	if init.openCalled != 0 {
		t.Errorf("initiator should not have been called")
	}
}

// ---- Cost Spike -----------------------------------------------------

type fakeNotifier struct{ events []SpikeEvent }

func (f *fakeNotifier) NotifySpike(_ context.Context, _ tenancy.Context, e SpikeEvent) error {
	f.events = append(f.events, e)
	return nil
}

func TestCostSpike_FiresAboveThreshold(t *testing.T) {
	n := &fakeNotifier{}
	wf := CostSpike{Notifier: n}
	payload, _ := json.Marshal(CostSpikePayload{
		WorkloadID:        "wl-1",
		ObservedDeltaUSD:  120,
		MinDeltaThreshold: 50,
	})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload); err != nil {
		t.Fatal(err)
	}
	if len(n.events) != 1 {
		t.Errorf("events = %d, want 1", len(n.events))
	}
}

func TestCostSpike_SilentBelowThreshold(t *testing.T) {
	n := &fakeNotifier{}
	wf := CostSpike{Notifier: n}
	payload, _ := json.Marshal(CostSpikePayload{
		WorkloadID:        "wl-1",
		ObservedDeltaUSD:  5,
		MinDeltaThreshold: 50,
	})
	if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload); err != nil {
		t.Fatal(err)
	}
	if len(n.events) != 0 {
		t.Errorf("events = %d, want 0 (below threshold)", len(n.events))
	}
}

// ---- Registration smoke ---------------------------------------------

func TestAllWorkflows_RegisterableTogether(t *testing.T) {
	disp := worker.NewInMemory()
	iss, _, _ := receipts.GenerateIssuer("k1")
	ws := []worker.Workflow{
		ApplyFix{Composer: &agent.Composer{LLM: &agent.FakeLLMClient{}}, Publisher: &fakePublisher{}},
		ReceiptIssue{Issuer: iss, Store: &fakeReceiptStore{}},
		RollbackWatchdog{Initiator: &fakeInitiator{}},
		CostSpike{Notifier: &fakeNotifier{}},
	}
	for _, w := range ws {
		if err := disp.Register(w); err != nil {
			t.Fatalf("register %q: %v", w.Name(), err)
		}
	}
	if got := disp.Workflows(); len(got) != 4 {
		t.Errorf("registered %v, want 4", got)
	}
}
