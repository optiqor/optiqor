package prwriter

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// MaxPreviewBytes caps the size of an /v1/apply-fixes request body.
const MaxPreviewBytes = 1 << 20

// PreviewRequest is the wire input. The TenantHeader middleware
// extracts the tenant id and stuffs it in context; this handler
// reads from there.
type PreviewRequest struct {
	Chart     string        `json:"chart"`
	Workload  string        `json:"workload"`
	ChartYAML string        `json:"chart_yaml"`
	Model     string        `json:"model"`
	Finding   rules.Finding `json:"finding"`
}

// PreviewResponse echoes the rendered PR body + extracted diff + the
// sanitizer result so the operator can confirm the LLM saw clean
// input.
type PreviewResponse struct {
	MarkdownBody string         `json:"markdown_body"`
	UnifiedDiff  string         `json:"unified_diff"`
	Explanation  string         `json:"explanation"`
	Sanitizer    map[string]any `json:"sanitizer"`
}

// Handler serves POST /v1/apply-fixes. The handler doesn't open a
// GitHub PR yet — that's a Phase-3 add — it returns what *would* be
// posted so the customer can review.
type Handler struct {
	Composer *agent.Composer
}

// Preview composes the markdown body + LLM-generated diff and
// returns both so the customer can review before authorising a real
// PR open.
func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Composer == nil {
		http.Error(w, "composer not configured", http.StatusInternalServerError)
		return
	}
	tCtx, err := tenancy.FromContext(r.Context())
	if err != nil {
		http.Error(w, "missing tenant context", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxPreviewBytes))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer func() { _ = r.Body.Close() }()
	var req PreviewRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Chart == "" || req.ChartYAML == "" {
		http.Error(w, "chart and chart_yaml required", http.StatusBadRequest)
		return
	}

	fix, err := h.Composer.GenerateFix(r.Context(), tCtx, agent.FixRequest{
		Finding:   req.Finding,
		ChartYAML: req.ChartYAML,
		Workload:  req.Workload,
		Model:     req.Model,
	})
	if err != nil {
		http.Error(w, "compose: "+err.Error(), http.StatusBadGateway)
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
		SecurityVisible:        req.Finding.Category == rules.CategorySecurity,
	})
	if err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
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

// Mount registers the route on the supplied mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/apply-fixes", h.Preview)
}
