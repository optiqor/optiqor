package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// FakeLLMClient is the deterministic in-process LLMClient used by
// tests and by Phase-1 dev mode. It replays scripted responses and
// records every call so assertions can verify the orchestrator passed
// the right prompts.
type FakeLLMClient struct {
	mu        sync.Mutex
	Responses []LLMResponse // FIFO queue; one Generate returns one entry
	Calls     []LLMRequest  // captured for assertions
	Err       error         // when non-nil, Generate returns this instead of a Response
}

// Generate returns the next scripted response, or Err if set, or an
// error if the queue is empty. Always thread-safe.
func (f *FakeLLMClient) Generate(_ context.Context, req LLMRequest) (LLMResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, req)
	if f.Err != nil {
		return LLMResponse{}, f.Err
	}
	if len(f.Responses) == 0 {
		return LLMResponse{}, errors.New("fake llm: no scripted response left")
	}
	r := f.Responses[0]
	f.Responses = f.Responses[1:]
	return r, nil
}

// FakeRecorder captures CallRecord entries so tests can assert
// attribution happened. Implements [BudgetRecorder].
type FakeRecorder struct {
	mu      sync.Mutex
	Records []CallRecord
}

// Record stores the call record.
func (r *FakeRecorder) Record(_ context.Context, _ tenancy.Context, c CallRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Records = append(r.Records, c)
	return nil
}
