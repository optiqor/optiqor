package prwriter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/optiqor/backend/internal/agent"
	"github.com/optiqor/backend/internal/tenancy"
	"github.com/optiqor/optiqor-cli/pkg/rules"
)

func mkPreviewReq() PreviewRequest {
	return PreviewRequest{
		Chart:     "charts/api",
		Workload:  "api",
		ChartYAML: "api:\n  resources:\n    requests:\n      cpu: 2",
		Model:     "claude-sonnet",
		Finding: rules.Finding{
			DetectorID: "cpu-overprovisioned",
			Workload:   "api",
			Title:      "CPU overprovisioned",
			Severity:   rules.SeverityMed,
			Category:   rules.CategoryCost,
		},
	}
}

func TestPreview_HappyPath_ReturnsBodyAndDiff(t *testing.T) {
	llm := &agent.FakeLLMClient{Responses: []agent.LLMResponse{{
		Text: "EXPLANATION:\nfix cpu\nDIFF:\n--- a\n+++ b\n@@\n-cpu: 2\n+cpu: 1\n",
	}}}
	h := &Handler{Composer: &agent.Composer{LLM: llm}}

	body, _ := json.Marshal(mkPreviewReq())
	req := httptest.NewRequest(http.MethodPost, "/v1/apply-fixes", strings.NewReader(string(body)))
	req = req.WithContext(tenancy.WithContext(context.Background(), tenancy.Context{TenantID: "t1"}))
	w := httptest.NewRecorder()
	h.Preview(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", w.Code, w.Body.String())
	}
	var resp PreviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.MarkdownBody, "Optiqor analysis") {
		t.Errorf("markdown body missing header:\n%s", resp.MarkdownBody)
	}
	if !strings.Contains(resp.UnifiedDiff, "--- a") {
		t.Errorf("diff missing")
	}
}

func TestPreview_NoTenant_401(t *testing.T) {
	h := &Handler{Composer: &agent.Composer{LLM: &agent.FakeLLMClient{}}}
	body, _ := json.Marshal(mkPreviewReq())
	req := httptest.NewRequest(http.MethodPost, "/v1/apply-fixes", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.Preview(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d", w.Code)
	}
}

func TestPreview_RejectsNonPost(t *testing.T) {
	h := &Handler{Composer: &agent.Composer{LLM: &agent.FakeLLMClient{}}}
	req := httptest.NewRequest(http.MethodGet, "/v1/apply-fixes", nil)
	w := httptest.NewRecorder()
	h.Preview(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d", w.Code)
	}
}

func TestPreview_RequiresChartFields(t *testing.T) {
	h := &Handler{Composer: &agent.Composer{LLM: &agent.FakeLLMClient{}}}
	body, _ := json.Marshal(PreviewRequest{})
	req := httptest.NewRequest(http.MethodPost, "/v1/apply-fixes", strings.NewReader(string(body)))
	req = req.WithContext(tenancy.WithContext(context.Background(), tenancy.Context{TenantID: "t1"}))
	w := httptest.NewRecorder()
	h.Preview(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d", w.Code)
	}
}
