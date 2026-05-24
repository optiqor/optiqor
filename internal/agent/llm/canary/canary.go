// Package canary fans an LLM call out to a Primary client (the one
// whose output Composer uses) and a Secondary one, then compares the
// two diffs and emits a divergence alert when they disagree. Catches
// hallucination patterns that pass the deterministic post-validator —
// e.g. both models drop the same label, but one introduces a phantom
// volumeMount.
//
// Sample rate is configured per call: 100% gives canary on every
// request, 5% rides on the same prompt cache prefix. Cost-conscious
// callers route Secondary to Haiku.
package canary

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// SampleDecider gates the canary; returns true when this call should
// be mirrored. Pure func + injected so tests can pin sampling.
type SampleDecider func(req agent.LLMRequest) bool

// AlwaysSample mirrors every call.
func AlwaysSample(_ agent.LLMRequest) bool { return true }

// NeverSample disables the canary.
func NeverSample(_ agent.LLMRequest) bool { return false }

// Reporter receives every divergence (or non-divergence when
// Always=true). Production wires this to Sentry; tests collect.
type Reporter interface {
	Report(ctx context.Context, t tenancy.Context, ev Event)
}

type Event struct {
	PrimaryModel   string
	SecondaryModel string
	Diverged       bool
	Reason         string
	Workload       string
}

// NullReporter drops every event; useful as a default.
type NullReporter struct{}

func (NullReporter) Report(_ context.Context, _ tenancy.Context, _ Event) {}

// Client wraps two LLMClient adapters. Primary's response is the one
// Composer sees; Secondary runs in a goroutine when SampleDecider
// returns true. Errors from Secondary never propagate back to the
// caller — divergence reporting is best-effort.
type Client struct {
	Primary   agent.LLMClient
	Secondary agent.LLMClient
	Sampler   SampleDecider
	Reporter  Reporter
	Workload  func(agent.LLMRequest) string // optional extractor for the event tag

	mu     sync.Mutex
	lastEv *Event // exposed via LastEvent for tests
}

func New(primary, secondary agent.LLMClient, sampler SampleDecider, reporter Reporter) *Client {
	if primary == nil {
		panic("canary: nil primary")
	}
	if sampler == nil {
		sampler = NeverSample
	}
	if reporter == nil {
		reporter = NullReporter{}
	}
	return &Client{Primary: primary, Secondary: secondary, Sampler: sampler, Reporter: reporter}
}

// Generate runs Primary inline; spawns Secondary in a goroutine when
// the sampler approves; reports divergence after both finish. Returns
// Primary's response so Composer behaviour is unchanged when the
// canary is off.
func (c *Client) Generate(ctx context.Context, req agent.LLMRequest) (agent.LLMResponse, error) {
	primary, err := c.Primary.Generate(ctx, req)
	if err != nil {
		return primary, err
	}
	if c.Secondary == nil || !c.Sampler(req) {
		return primary, nil
	}

	t, _ := tenancy.FromContext(ctx)
	go c.compare(ctx, t, req, primary)
	return primary, nil
}

func (c *Client) compare(ctx context.Context, t tenancy.Context, req agent.LLMRequest, primary agent.LLMResponse) {
	secondary, err := c.Secondary.Generate(ctx, req)
	ev := Event{
		PrimaryModel:   primary.Model,
		SecondaryModel: secondary.Model,
	}
	if c.Workload != nil {
		ev.Workload = c.Workload(req)
	}
	switch {
	case err != nil:
		ev.Diverged = true
		ev.Reason = "secondary error: " + err.Error()
	case extractDiff(primary.Text) == "" || extractDiff(secondary.Text) == "":
		ev.Diverged = true
		ev.Reason = "one model produced no diff"
	case extractDiff(primary.Text) != extractDiff(secondary.Text):
		ev.Diverged = true
		ev.Reason = "diff body differs across models"
	}
	c.mu.Lock()
	cp := ev
	c.lastEv = &cp
	c.mu.Unlock()
	c.Reporter.Report(ctx, t, ev)
}

// LastEvent returns the most recent divergence event, nil if none.
// Tests use this; production reads via Reporter only.
func (c *Client) LastEvent() *Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastEv == nil {
		return nil
	}
	cp := *c.lastEv
	return &cp
}

// extractDiff pulls everything after "DIFF:" — same shape the Composer
// uses internally. Local copy keeps this package free of imports
// outside agent/.
func extractDiff(s string) string {
	idx := strings.Index(s, "DIFF:")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(s[idx+len("DIFF:"):])
}

// ErrNoSecondary signals to callers that a canary was requested
// without a secondary adapter. NeverSample makes the canary a no-op
// instead, but an explicit sampler + missing Secondary should fail
// loudly in tests.
var ErrNoSecondary = errors.New("canary: secondary client not configured")
