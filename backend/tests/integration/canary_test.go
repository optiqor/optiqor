//go:build integration

package integration

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/agent/llm/canary"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// TestCanary_DivergenceReportedThroughComposerSurface stands a Canary
// in front of two FakeLLMClients and asserts the Reporter sees the
// divergence event while Composer still gets Primary's response.
func TestCanary_DivergenceReportedThroughComposerSurface(t *testing.T) {
	primary := &agent.FakeLLMClient{Responses: []agent.LLMResponse{{
		Text:  "EXPLANATION:\nfix CPU\nDIFF:\n--- a\n+++ b\n@@\n-cpu: 2\n+cpu: 1",
		Model: "claude-sonnet",
	}}}
	secondary := &agent.FakeLLMClient{Responses: []agent.LLMResponse{{
		Text:  "EXPLANATION:\nfix CPU\nDIFF:\n--- a\n+++ b\n@@\n-cpu: 2\n+cpu: 500m",
		Model: "claude-haiku",
	}}}
	rep := &collectingReporter{}
	c := canary.New(primary, secondary, canary.AlwaysSample, rep)

	resp, err := c.Generate(context.Background(), agent.LLMRequest{System: "s", User: "u"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Model != "claude-sonnet" {
		t.Errorf("primary model lost: %q", resp.Model)
	}

	waitFor(t, 500*time.Millisecond, func() bool { return rep.count() == 1 })
	ev := rep.last()
	if !ev.Diverged {
		t.Errorf("expected divergence; got %+v", ev)
	}
	if !strings.Contains(ev.Reason, "differs") {
		t.Errorf("Reason = %q", ev.Reason)
	}
}

func TestCanary_MatchingDiffsAreNotDivergent(t *testing.T) {
	primary := &agent.FakeLLMClient{Responses: []agent.LLMResponse{{Text: "EXPLANATION:\nx\nDIFF:\nSAME", Model: "sonnet"}}}
	secondary := &agent.FakeLLMClient{Responses: []agent.LLMResponse{{Text: "EXPLANATION:\ny\nDIFF:\nSAME", Model: "haiku"}}}
	rep := &collectingReporter{}
	c := canary.New(primary, secondary, canary.AlwaysSample, rep)
	if _, err := c.Generate(context.Background(), agent.LLMRequest{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 500*time.Millisecond, func() bool { return rep.count() == 1 })
	if rep.last().Diverged {
		t.Errorf("matching diffs flagged as diverged: %+v", rep.last())
	}
}

type collectingReporter struct {
	mu sync.Mutex
	ev []canary.Event
}

func (r *collectingReporter) Report(_ context.Context, _ tenancy.Context, ev canary.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ev = append(r.ev, ev)
}
func (r *collectingReporter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ev)
}
func (r *collectingReporter) last() canary.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ev[len(r.ev)-1]
}

func waitFor(t *testing.T, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waited %s; condition still false", d)
}
