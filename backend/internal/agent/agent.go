// Package agent turns a rules.Finding into an explanation + unified
// values.yaml diff via an LLM. Sanitises input through
// internal/agent/llm/sanitizer; enforces the per-call $0.40 cap
// before any network egress; records every call against the
// llm_calls table. Does not touch the GitHub API — that lives in
// cmd/api once the Apply Fix workflow opens the PR.
package agent

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/agent/llm/sanitizer"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// LLMRequest stays small so adapters only marshal; prompt
// construction is the orchestrator's job.
//
// CachedSystem and System are sent as separate system blocks: the
// Anthropic adapter marks CachedSystem with cache_control: ephemeral so
// repeated calls hit the prompt cache. Keep CachedSystem byte-stable
// across calls — Anthropic keys the cache on equality.
type LLMRequest struct {
	CachedSystem string
	System       string
	User         string
	Model        string // adapter-specific identifier (e.g. "claude-sonnet-4-6")
	MaxTokens    int
}

type LLMResponse struct {
	Text              string
	InputTokens       int
	OutputTokens      int
	CacheReadTokens   int // tokens served from the prompt cache (a hit)
	CacheCreateTokens int // tokens written into the prompt cache (cold miss)
	CostUSDCents      int64
	Model             string
}

// LLMClient is the seam between the orchestrator and any model. Real
// adapters live behind build tags; tests use FakeLLMClient.
type LLMClient interface {
	Generate(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

type FixRequest struct {
	Finding    rules.Finding
	ChartYAML  string
	Model      string
	Workload   string
	SystemHint string // optional extra context from the calling workflow
}

type FixResponse struct {
	Explanation  string
	UnifiedDiff  string
	CostUSDCents int64
	Model        string
	Sanitised    sanitizer.Result
}

// Budget enforces the per-call cost cap from CLAUDE.md ($0.40). Cents
// are the wire unit.
type Budget struct {
	PerCallCents int64
}

// BudgetRecorder writes every call to the llm_calls table for
// attribution.
type BudgetRecorder interface {
	Record(ctx context.Context, t tenancy.Context, call CallRecord) error
}

type CallRecord struct {
	Workload       string
	Purpose        string // "apply-fix" | "narrative" | "classify" | …
	Model          string
	InputTokens    int
	OutputTokens   int
	CacheHitTokens int // matches llm_calls.cache_hit_tokens
	CostUSDCents   int64
	DurationMS     int
	InputHash      [32]byte
	OutputHash     [32]byte
	Suspicious     bool
}

// Composer is safe for concurrent use as long as its dependencies are.
type Composer struct {
	LLM      LLMClient
	Budget   Budget
	Recorder BudgetRecorder
}

var ErrNilLLM = errors.New("agent: nil LLMClient")

// ErrBudgetExceeded is the one place money-spend decisions happen — if
// the projected cost exceeds the cap we refuse the call before any
// network egress.
var ErrBudgetExceeded = errors.New("agent: per-call budget exceeded")

// ErrInjection fires only when a workflow has opted out of the default
// wrap-and-continue policy in favour of fail-closed. Most callers wrap.
var ErrInjection = errors.New("agent: prompt-injection markers detected")

// GenerateFix returns a FixResponse ready to feed prwriter.Render.
func (c *Composer) GenerateFix(ctx context.Context, t tenancy.Context, req FixRequest) (FixResponse, error) {
	if c.LLM == nil {
		return FixResponse{}, ErrNilLLM
	}
	if err := t.Validate(); err != nil {
		return FixResponse{}, err
	}

	san, err := sanitizer.Sanitize(req.ChartYAML, sanitizer.Options{})
	if err != nil {
		return FixResponse{}, fmt.Errorf("agent: sanitize: %w", err)
	}

	cached := buildCachedSystem(req)
	system := buildSystem(req)
	user := buildUser(req, san.Output)

	// Worst-case projection — never pay for a call we've already
	// decided is too expensive. Assumes a cold cache (no read discount)
	// so a partial cache hit never tips us over the cap unexpectedly.
	if c.Budget.PerCallCents > 0 {
		projected := projectedCostCents(req.Model, len(cached)+len(system)+len(user), req.maxTokens())
		if projected > c.Budget.PerCallCents {
			return FixResponse{}, fmt.Errorf("%w: projected %d cents > cap %d", ErrBudgetExceeded, projected, c.Budget.PerCallCents)
		}
	}

	start := time.Now()
	resp, err := c.LLM.Generate(ctx, LLMRequest{
		CachedSystem: cached,
		System:       system,
		User:         user,
		Model:        req.Model,
		MaxTokens:    req.maxTokens(),
	})
	if err != nil {
		return FixResponse{}, fmt.Errorf("agent: llm: %w", err)
	}

	out := FixResponse{
		Explanation:  extractExplanation(resp.Text),
		UnifiedDiff:  extractDiff(resp.Text),
		CostUSDCents: resp.CostUSDCents,
		Model:        resp.Model,
		Sanitised:    san,
	}

	if c.Recorder != nil {
		_ = c.Recorder.Record(ctx, t, CallRecord{
			Workload:       req.Workload,
			Purpose:        "apply-fix",
			Model:          resp.Model,
			InputTokens:    resp.InputTokens,
			OutputTokens:   resp.OutputTokens,
			CacheHitTokens: resp.CacheReadTokens,
			CostUSDCents:   resp.CostUSDCents,
			DurationMS:     int(time.Since(start) / time.Millisecond),
			InputHash:      sha256.Sum256([]byte(cached + system + user)),
			OutputHash:     sha256.Sum256([]byte(resp.Text)),
			Suspicious:     san.Suspicious,
		})
	}
	return out, nil
}

func (r FixRequest) maxTokens() int {
	// 4k handles a unified diff comfortably on the Sonnet workhorse.
	return 4096
}

// buildCachedSystem is the stable prefix every Anthropic call shares.
// Must be byte-identical across calls — Anthropic prompt caching keys
// on equality. Adding a per-call value here defeats caching; that goes
// to buildSystem instead.
func buildCachedSystem(_ FixRequest) string {
	var b strings.Builder
	b.WriteString("You are Optiqor, a deterministic Kubernetes cost-and-security review assistant. ")
	b.WriteString("Output two sections labelled `EXPLANATION:` and `DIFF:`. ")
	b.WriteString("The diff must be a valid unified diff against the supplied chart values.yaml; nothing else. ")
	b.WriteString("Never invent fields that do not appear in the input.")
	return b.String()
}

// buildSystem is the per-call addendum (workflow-supplied hint).
// Empty for most calls; populated only when the caller has a stable
// hint that genuinely varies per request.
func buildSystem(req FixRequest) string {
	return req.SystemHint
}

func buildUser(req FixRequest, chart string) string {
	var b strings.Builder
	b.WriteString("Workload: ")
	b.WriteString(req.Workload)
	b.WriteString("\nDetector: ")
	b.WriteString(req.Finding.DetectorID)
	b.WriteString("\nTitle: ")
	b.WriteString(req.Finding.Title)
	b.WriteString("\nSeverity: ")
	b.WriteString(string(req.Finding.Severity))
	b.WriteString("\nDetail:\n")
	b.WriteString(req.Finding.Detail)
	b.WriteString("\n\nChart values.yaml:\n")
	b.WriteString(chart)
	return b.String()
}

// extractExplanation falls back to raw text when the model ignored
// the EXPLANATION:/DIFF: protocol, so the workflow always has
// something to render.
func extractExplanation(s string) string {
	return cutSection(s, "EXPLANATION:", "DIFF:")
}

func extractDiff(s string) string {
	idx := strings.Index(s, "DIFF:")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(s[idx+len("DIFF:"):])
}

func cutSection(s, start, end string) string {
	a := strings.Index(s, start)
	if a < 0 {
		return strings.TrimSpace(s)
	}
	a += len(start)
	b := strings.Index(s[a:], end)
	if b < 0 {
		return strings.TrimSpace(s[a:])
	}
	return strings.TrimSpace(s[a : a+b])
}

// projectedCostCents is the budget gate's worst-case estimator —
// update when Anthropic prices change.
func projectedCostCents(model string, inputChars, maxOutputTokens int) int64 {
	// 1 token ≈ 4 input chars for English.
	inputTokens := int64(inputChars) / 4
	outputTokens := int64(maxOutputTokens)
	in, out := perMillionCents(model)
	cents := (inputTokens*in + outputTokens*out) / 1_000_000
	if cents < 1 {
		return 1
	}
	return cents
}

// perMillionCents returns input + output cents per million tokens.
// Unknown models default to Sonnet pricing so the gate never
// undercharges.
func perMillionCents(model string) (in, out int64) {
	switch normaliseModel(model) {
	case "claude-haiku":
		return 100, 500 // $1 / $5
	case "claude-sonnet":
		return 300, 1500 // $3 / $15
	case "claude-opus":
		return 1500, 7500 // $15 / $75
	}
	return 300, 1500
}

func normaliseModel(m string) string {
	m = strings.ToLower(m)
	switch {
	case strings.Contains(m, "haiku"):
		return "claude-haiku"
	case strings.Contains(m, "opus"):
		return "claude-opus"
	case strings.Contains(m, "sonnet"):
		return "claude-sonnet"
	}
	return "claude-sonnet"
}

// SortedModels has stable order for /v1/models metadata diff stability.
func SortedModels() []string {
	out := []string{"claude-haiku", "claude-sonnet", "claude-opus"}
	sort.Strings(out)
	return out
}
