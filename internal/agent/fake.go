package agent

import (
	"context"
	"errors"
	"sync"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// FakeLLMClient replays scripted Responses in FIFO order and captures
// every request for assertion. Used by tests and Phase-1 dev mode.
type FakeLLMClient struct {
	mu        sync.Mutex
	Responses []LLMResponse
	Calls     []LLMRequest
	Err       error // when set, Generate returns this instead of a Response
}

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

// FakeRecorder captures CallRecord entries for attribution tests.
type FakeRecorder struct {
	mu      sync.Mutex
	Records []CallRecord
}

func (r *FakeRecorder) Record(_ context.Context, _ tenancy.Context, c CallRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Records = append(r.Records, c)
	return nil
}
