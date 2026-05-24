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

func TestApplyFix_Execute(t *testing.T) {
	happyPayload, _ := json.Marshal(ApplyFixPayload{
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
	badJSONPayload := []byte("not json")
	llmFailPayload, _ := json.Marshal(ApplyFixPayload{
		ChartYAML: "api: {}",
		Finding:   rules.Finding{Workload: "api", Title: "x"},
	})

	for _, tc := range []struct {
		name    string
		llm     *agent.FakeLLMClient
		payload []byte
		wantErr bool
		check   func(t *testing.T, p *fakePublisher)
	}{
		{
			name: "happy path publishes pr",
			llm: &agent.FakeLLMClient{Responses: []agent.LLMResponse{{
				Text:  "EXPLANATION:\nfix CPU\nDIFF:\n--- a\n+++ b\n@@\n- cpu: 2\n+ cpu: 1\n",
				Model: "claude-sonnet",
			}}},
			payload: happyPayload,
			check: func(t *testing.T, p *fakePublisher) {
				t.Helper()
				if len(p.called) != 1 {
					t.Fatalf("publisher called %d times", len(p.called))
				}
				pr := p.called[0]
				if pr.RepoOwner != "acme" || pr.RepoName != "api" {
					t.Errorf("pr coords: %+v", pr)
				}
				if pr.HeadBranch != "optiqor/apply-fix/afix_1" {
					t.Errorf("head branch = %q", pr.HeadBranch)
				}
				if pr.Body == "" {
					t.Error("PR body empty")
				}
			},
		},
		{
			name:    "llm failure propagates",
			llm:     &agent.FakeLLMClient{Err: errors.New("rate limit")},
			payload: llmFailPayload,
			wantErr: true,
		},
		{
			name:    "bad json rejected",
			llm:     &agent.FakeLLMClient{},
			payload: badJSONPayload,
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			wf := ApplyFix{Composer: &agent.Composer{LLM: tc.llm}, Publisher: pub}
			err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, tc.payload)
			if tc.wantErr {
				if err == nil {
					t.Error("want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if tc.check != nil {
				tc.check(t, pub)
			}
		})
	}
}

func TestApplyFix_DispatcherRoundTrip_PublishesPR(t *testing.T) {
	llm := &agent.FakeLLMClient{Responses: []agent.LLMResponse{{
		Text:  "EXPLANATION:\nfix CPU\nDIFF:\n--- a\n+++ b\n@@\n- cpu: 2\n+ cpu: 1\n",
		Model: "claude-sonnet",
	}}}
	pub := &fakePublisher{}
	wf := ApplyFix{Composer: &agent.Composer{LLM: llm}, Publisher: pub}
	disp := worker.NewInMemory()
	if err := disp.Register(wf); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(ApplyFixPayload{
		RepoOwner: "acme", RepoName: "api", BaseBranch: "main", ChartPath: "charts/api",
		ChartYAML: "api:\n  resources: {requests: {cpu: 2}}",
		Model:     "claude-sonnet",
		Finding: rules.Finding{
			DetectorID: "cpu-overprovisioned", Workload: "api", Title: "CPU overprovisioned",
			Severity: rules.SeverityMed, Category: rules.CategoryCost,
		},
		ApplyFixID: "afix_1",
		Now:        time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC),
	})
	if err := disp.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, worker.QueueDefault, "apply_fix", payload); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if len(pub.called) != 1 {
		t.Fatalf("publisher called %d times", len(pub.called))
	}
}

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

func TestRollbackWatchdog_Execute(t *testing.T) {
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		bounds    []rollback.Bound
		snapValue float64
		wantOpen  int
		wantFixID string
	}{
		{
			name:      "triggers when bound crossed",
			bounds:    []rollback.Bound{{Kind: rollback.SignalLatencyP95, RealMax: 100, Baseline: 50}},
			snapValue: 250,
			wantOpen:  1,
			wantFixID: "afix_99",
		},
		{
			name:      "no action when within bounds",
			bounds:    []rollback.Bound{{Kind: rollback.SignalLatencyP95, RealMax: 1000}},
			snapValue: 100,
			wantOpen:  0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := rollback.State{
				ApplyFixID: "afix_99",
				MergedAt:   now,
				Bounds:     tc.bounds,
			}
			init := &fakeInitiator{}
			wf := RollbackWatchdog{Initiator: init}
			payload, _ := json.Marshal(RollbackWatchdogPayload{
				State:    state,
				Snapshot: rollback.Snapshot{Kind: rollback.SignalLatencyP95, Value: tc.snapValue, ObservedAt: now.Add(time.Hour)},
				Now:      now.Add(time.Hour),
			})
			if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if init.openCalled != tc.wantOpen {
				t.Errorf("openCalled = %d, want %d", init.openCalled, tc.wantOpen)
			}
			if tc.wantFixID != "" {
				if len(init.opened) != 1 || init.opened[0] != tc.wantFixID {
					t.Errorf("opened = %+v, want [%q]", init.opened, tc.wantFixID)
				}
			}
		})
	}
}

type fakeNotifier struct{ events []SpikeEvent }

func (f *fakeNotifier) NotifySpike(_ context.Context, _ tenancy.Context, e SpikeEvent) error {
	f.events = append(f.events, e)
	return nil
}

func TestCostSpike_Execute(t *testing.T) {
	for _, tc := range []struct {
		name      string
		delta     float64
		threshold float64
		wantFires int
	}{
		{"fires above threshold", 120, 50, 1},
		{"silent below threshold", 5, 50, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &fakeNotifier{}
			wf := CostSpike{Notifier: n}
			payload, _ := json.Marshal(CostSpikePayload{
				WorkloadID:        "wl-1",
				ObservedDeltaUSD:  tc.delta,
				MinDeltaThreshold: tc.threshold,
			})
			if err := wf.Execute(context.Background(), tenancy.Context{TenantID: "t1"}, payload); err != nil {
				t.Fatal(err)
			}
			if len(n.events) != tc.wantFires {
				t.Errorf("events = %d, want %d", len(n.events), tc.wantFires)
			}
		})
	}
}

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
