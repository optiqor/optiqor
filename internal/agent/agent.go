// Package agent is the SaaS-side LLM orchestrator that turns a
// [rules.Finding] into a human-readable explanation and a unified
// `values.yaml` diff suggesting the fix.
//
// Phase 1 contract:
//
//   - Inputs are sanitised via internal/agent/llm/sanitizer before
//     leaving the boundary. Customer secrets, file paths, and prompt
//     injection markers are stripped or wrapped.
//   - The LLM call goes through an [LLMClient] interface so:
//   - tests run against a deterministic [FakeLLMClient];
//   - the real Anthropic SDK adapter ships behind an env flag
//     without forcing every test path to depend on it.
//   - The Composer enforces a per-call cost cap (see [Budget]). Calls
//     that would exceed the cap return [ErrBudgetExceeded] before any
//     network egress.
//   - Every call records the cost via [BudgetRecorder.Record] so the
//     llm_calls table captures the running total per tenant.
//
// The package deliberately does NOT depend on the GitHub API. PR
// opening lives in cmd/api once the GitHub App credentials are wired;
// this package is responsible only for the structured outputs that
// feed into prwriter and into the Apply Fix workflow.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/agent/llm/sanitizer"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// LLMRequest is the shape every model adapter consumes. Keep it small;
// the orchestrator owns prompt construction, not the caller.
type LLMRequest struct {
	System    string
	User      string
	Model     string // adapter-specific identifier (e.g. "claude-sonnet-4-6")
	MaxTokens int
}

// LLMResponse is what the adapter returns. The orchestrator decides
// what to do with it; the adapter only marshals the call.
type LLMResponse struct {
	Text         string
	InputTokens  int
	OutputTokens int
	CostUSDCents int64
	Model        string
}

// LLMClient is the seam between the orchestrator and any model. Real
// implementations live behind build tags (`agent_anthropic.go`); tests
// use [FakeLLMClient].
type LLMClient interface {
	Generate(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

// FixRequest is the orchestrator's input. A finding + the original
// chart bytes; everything else is derived.
type FixRequest struct {
	Finding    rules.Finding
	ChartYAML  string
	Model      string
	Workload   string
	SystemHint string // optional extra context from the calling workflow
}

// FixResponse is the orchestrator's output. The PR-writer renders the
// markdown; this package only commits to the structured fields.
type FixResponse struct {
	Explanation  string
	UnifiedDiff  string
	CostUSDCents int64
	Model        string
	Sanitised    sanitizer.Result
}

// Budget enforces the per-call cost cap stated in backend CLAUDE.md
// ("Cost cap per analysis: $0.40"). Cents are the wire unit.
type Budget struct {
	PerCallCents int64
}

// BudgetRecorder persists every LLM call's cost for attribution. The
// production implementation writes to the llm_calls table.
type BudgetRecorder interface {
	Record(ctx context.Context, t tenancy.Context, call CallRecord) error
}

// CallRecord is one row in the llm_calls table.
type CallRecord struct {
	Workload     string
	Model        string
	InputTokens  int
	OutputTokens int
	CostUSDCents int64
	Suspicious   bool
}

// Composer wires the sanitizer, LLM client, budget guard, and
// recorder. Construct once per process; safe for concurrent use as
// long as the LLMClient and BudgetRecorder are.
type Composer struct {
	LLM      LLMClient
	Budget   Budget
	Recorder BudgetRecorder
}

// ErrNilLLM is returned when Compose is called without an LLM.
var ErrNilLLM = errors.New("agent: nil LLMClient")

// ErrBudgetExceeded is returned when a call's projected cost would
// exceed the per-call cap. The composer refuses to call the LLM in
// this case — the budget gate is the only place that decides whether
// money gets spent.
var ErrBudgetExceeded = errors.New("agent: per-call budget exceeded")

// ErrInjection is returned when the sanitizer flags injection markers
// AND the calling workflow has opted to refuse rather than wrap. The
// default policy is to wrap (so the LLM treats the input as untrusted
// data); workflows that handle very sensitive material flip the
// switch.
var ErrInjection = errors.New("agent: prompt-injection markers detected")

// GenerateFix orchestrates one call. The Composer is responsible for:
//
//  1. sanitising the chart YAML (PII strip + injection wrap);
//  2. building the system + user prompts;
//  3. enforcing the budget;
//  4. invoking the LLMClient;
//  5. recording the call for attribution.
//
// The returned FixResponse is suitable as input to prwriter.Render.
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

	system := buildSystem(req)
	user := buildUser(req, san.Output)

	// Budget gate. We use a worst-case token estimate (input + max
	// output) and the model's listed rate so we never pay for a call
	// we've already decided is too expensive.
	if c.Budget.PerCallCents > 0 {
		projected := projectedCostCents(req.Model, len(system)+len(user), req.maxTokens())
		if projected > c.Budget.PerCallCents {
			return FixResponse{}, fmt.Errorf("%w: projected %d cents > cap %d", ErrBudgetExceeded, projected, c.Budget.PerCallCents)
		}
	}

	resp, err := c.LLM.Generate(ctx, LLMRequest{
		System:    system,
		User:      user,
		Model:     req.Model,
		MaxTokens: req.maxTokens(),
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
			Workload:     req.Workload,
			Model:        resp.Model,
			InputTokens:  resp.InputTokens,
			OutputTokens: resp.OutputTokens,
			CostUSDCents: resp.CostUSDCents,
			Suspicious:   san.Suspicious,
		})
	}
	return out, nil
}

func (r FixRequest) maxTokens() int {
	// Phase 1 default; per CLAUDE.md, Sonnet is the workhorse and 4k
	// output handles a unified diff comfortably.
	return 4096
}

// buildSystem composes the cached prefix every Anthropic call shares.
// Keep this stable: Anthropic's prompt caching keys on byte-equality.
func buildSystem(req FixRequest) string {
	var b strings.Builder
	b.WriteString("You are Optiqor, a deterministic Kubernetes cost-and-security review assistant. ")
	b.WriteString("Output two sections labelled `EXPLANATION:` and `DIFF:`. ")
	b.WriteString("The diff must be a valid unified diff against the supplied chart values.yaml; nothing else. ")
	b.WriteString("Never invent fields that do not appear in the input. ")
	if req.SystemHint != "" {
		b.WriteString(req.SystemHint)
	}
	return b.String()
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

// extractExplanation pulls the `EXPLANATION:` section out of the
// LLM's response. If the model didn't follow the protocol we fall
// back to the raw text so the workflow still has something to render.
func extractExplanation(s string) string {
	return cutSection(s, "EXPLANATION:", "DIFF:")
}

// extractDiff pulls the `DIFF:` section. The diff is everything after
// the marker, trimmed.
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

// projectedCostCents is the budget gate's worst-case estimator. The
// numbers track the published Anthropic rates for the Phase-1 default
// model lineup; update when prices change.
func projectedCostCents(model string, inputChars, maxOutputTokens int) int64 {
	// Rough heuristic: 1 token ≈ 4 input chars for English.
	inputTokens := int64(inputChars) / 4
	outputTokens := int64(maxOutputTokens)
	in, out := perMillionCents(model)
	cents := (inputTokens*in + outputTokens*out) / 1_000_000
	if cents < 1 {
		return 1 // round up — a $0.00x call still costs the customer credit somewhere
	}
	return cents
}

// perMillionCents returns (input_cents_per_million_tokens, output_cents_per_million_tokens)
// for the configured model. Unknown models default to Sonnet pricing so
// the budget gate never silently undercharges.
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

// SortedModels returns the recognised model identifiers, useful in
// /v1/models metadata. Stable order for diff stability.
func SortedModels() []string {
	out := []string{"claude-haiku", "claude-sonnet", "claude-opus"}
	sort.Strings(out)
	return out
}
