package canary

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestClient_Generate(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name     string
		primary  agent.LLMResponse
		secResp  agent.LLMResponse
		secErr   error
		sampler  SampleDecider
		wantDiv  bool
		divCause string
		reported bool
	}{
		{
			name:     "sampler never -> no comparison",
			primary:  agent.LLMResponse{Text: "EXPLANATION:\nx\nDIFF:\n--- a\n+++ b\n@@\n-1\n+2", Model: "sonnet"},
			secResp:  agent.LLMResponse{Text: "EXPLANATION:\nx\nDIFF:\n--- a\n+++ b\n@@\n-9\n+9", Model: "haiku"},
			sampler:  NeverSample,
			reported: false,
		},
		{
			name:     "diffs identical -> not diverged",
			primary:  agent.LLMResponse{Text: "EXPLANATION:\na\nDIFF:\n--- a\n+++ b\n@@\n-1\n+2", Model: "sonnet"},
			secResp:  agent.LLMResponse{Text: "EXPLANATION:\nb\nDIFF:\n--- a\n+++ b\n@@\n-1\n+2", Model: "haiku"},
			sampler:  AlwaysSample,
			reported: true,
			wantDiv:  false,
		},
		{
			name:     "diffs differ -> diverged",
			primary:  agent.LLMResponse{Text: "EXPLANATION:\nx\nDIFF:\nA", Model: "sonnet"},
			secResp:  agent.LLMResponse{Text: "EXPLANATION:\nx\nDIFF:\nB", Model: "haiku"},
			sampler:  AlwaysSample,
			reported: true,
			wantDiv:  true,
			divCause: "differs",
		},
		{
			name:     "secondary errored -> diverged",
			primary:  agent.LLMResponse{Text: "EXPLANATION:\nx\nDIFF:\nA", Model: "sonnet"},
			secErr:   errors.New("upstream timeout"),
			sampler:  AlwaysSample,
			reported: true,
			wantDiv:  true,
			divCause: "secondary error",
		},
		{
			name:     "secondary empty diff -> diverged",
			primary:  agent.LLMResponse{Text: "EXPLANATION:\nx\nDIFF:\nA", Model: "sonnet"},
			secResp:  agent.LLMResponse{Text: "no diff here", Model: "haiku"},
			sampler:  AlwaysSample,
			reported: true,
			wantDiv:  true,
			divCause: "one model",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primary := &agent.FakeLLMClient{Responses: []agent.LLMResponse{tc.primary}}
			var secondary agent.LLMClient
			if tc.secErr != nil {
				secondary = errLLM{err: tc.secErr}
			} else {
				secondary = &agent.FakeLLMClient{Responses: []agent.LLMResponse{tc.secResp}}
			}
			reporter := &captureReporter{}
			c := New(primary, secondary, tc.sampler, reporter)
			if _, err := c.Generate(ctx, agent.LLMRequest{System: "s", User: "u"}); err != nil {
				t.Fatalf("Generate: %v", err)
			}
			// Goroutine; give it a moment.
			deadline := time.Now().Add(500 * time.Millisecond)
			for time.Now().Before(deadline) {
				if reporter.count() > 0 || !tc.reported {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if reporter.count() != boolToInt(tc.reported) {
				t.Fatalf("reporter.count = %d, want %d", reporter.count(), boolToInt(tc.reported))
			}
			if tc.reported {
				ev := reporter.last()
				if ev.Diverged != tc.wantDiv {
					t.Errorf("Diverged = %v, want %v (reason=%q)", ev.Diverged, tc.wantDiv, ev.Reason)
				}
				if tc.divCause != "" && !contains(ev.Reason, tc.divCause) {
					t.Errorf("Reason %q missing %q", ev.Reason, tc.divCause)
				}
			}
		})
	}
}

func TestNew_PanicsOnNilPrimary(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on nil primary")
		}
	}()
	New(nil, nil, NeverSample, NullReporter{})
}

func TestNew_NilSamplerDefaultsToNever(t *testing.T) {
	c := New(&agent.FakeLLMClient{Responses: []agent.LLMResponse{{Text: ""}}}, nil, nil, nil)
	if _, err := c.Generate(context.Background(), agent.LLMRequest{}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if c.LastEvent() != nil {
		t.Error("LastEvent should be nil with NeverSample default")
	}
}

func TestClient_PassesPrimaryError(t *testing.T) {
	c := New(errLLM{err: errors.New("primary down")}, nil, NeverSample, NullReporter{})
	_, err := c.Generate(context.Background(), agent.LLMRequest{})
	if err == nil || err.Error() != "primary down" {
		t.Errorf("err = %v, want primary down", err)
	}
}

type captureReporter struct {
	mu sync.Mutex
	ev []Event
}

func (r *captureReporter) Report(_ context.Context, _ tenancy.Context, ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ev = append(r.ev, ev)
}

func (r *captureReporter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ev)
}

func (r *captureReporter) last() Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ev[len(r.ev)-1]
}

type errLLM struct{ err error }

func (e errLLM) Generate(_ context.Context, _ agent.LLMRequest) (agent.LLMResponse, error) {
	return agent.LLMResponse{}, e.err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
