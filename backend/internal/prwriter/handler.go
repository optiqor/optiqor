package prwriter

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/platform/httperr"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// PreviewRequest is the wire input. Tenant id comes from context
// (TenantHeader middleware), not the body.
type PreviewRequest struct {
	Chart     string        `json:"chart"`
	Workload  string        `json:"workload"`
	ChartYAML string        `json:"chart_yaml"`
	Model     string        `json:"model"`
	Finding   rules.Finding `json:"finding"`
}

// PreviewResponse surfaces the sanitizer result alongside the rendered
// body and diff so the operator can confirm the LLM saw clean input.
type PreviewResponse struct {
	MarkdownBody string         `json:"markdown_body"`
	UnifiedDiff  string         `json:"unified_diff"`
	Explanation  string         `json:"explanation"`
	Sanitizer    map[string]any `json:"sanitizer"`
}

// Handler serves POST /v1/apply-fixes. Doesn't open a GitHub PR yet
// (Phase 3) — returns what would be posted for customer review.
type Handler struct {
	Composer *agent.Composer
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httperr.MethodNotAllowed(w, r, "POST")
		return
	}
	if h.Composer == nil {
		httperr.Internal(w, r, "Apply Fix is not configured on this api node")
		return
	}
	tCtx, err := tenancy.FromContext(r.Context())
	if err != nil {
		httperr.Unauthorized(w, r, "tenant context required to preview an Apply Fix")
		return
	}
	body := http.MaxBytesReader(w, r.Body, config.PRApplyFixMaxBytes)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req PreviewRequest
	if err := dec.Decode(&req); err != nil {
		if httperr.IsBodyTooLarge(err) {
			httperr.BodyTooLarge(w, r, config.PRApplyFixMaxBytes)
			return
		}
		httperr.InvalidJSON(w, r, err)
		return
	}
	if req.Chart == "" {
		httperr.MissingField(w, r, "chart")
		return
	}
	if req.ChartYAML == "" {
		httperr.MissingField(w, r, "chart_yaml")
		return
	}

	fix, err := h.Composer.GenerateFix(r.Context(), tCtx, agent.FixRequest{
		Finding:   req.Finding,
		ChartYAML: req.ChartYAML,
		Workload:  req.Workload,
		Model:     req.Model,
	})
	if err != nil {
		if errors.Is(err, agent.ErrBudgetExceeded) {
			httperr.WriteWithDetails(w, r, http.StatusPaymentRequired, "BUDGET_EXCEEDED",
				"Apply Fix would exceed the per-call cost cap",
				map[string]any{"per_call_cents": 40})
			return
		}
		httperr.Upstream(w, r, "LLM did not return a usable fix")
		return
	}
	md, err := Render(Comment{
		Chart:                  req.Chart,
		Tenant:                 tCtx.TenantID,
		Workloads:              1,
		Findings:               []rules.Finding{req.Finding},
		MonthlySavingsUSDCents: req.Finding.MonthlyUSDCents,
		AnnualSavingsUSDCents:  req.Finding.MonthlyUSDCents * 12,
		Mode:                   ModeAgent,
		UnifiedDiff:            fix.UnifiedDiff,
		Narrative:              fix.Explanation,
		SecurityVisible:        req.Finding.Category == rules.CategorySecurity,
	})
	if err != nil {
		httperr.Internal(w, r, "could not render PR comment")
		return
	}
	out := PreviewResponse{
		MarkdownBody: md,
		UnifiedDiff:  fix.UnifiedDiff,
		Explanation:  fix.Explanation,
		Sanitizer: map[string]any{
			"suspicious": fix.Sanitised.Suspicious,
			"reasons":    fix.Sanitised.Reasons,
			"truncated":  fix.Sanitised.Truncated,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/apply-fixes", h.Preview)
}
