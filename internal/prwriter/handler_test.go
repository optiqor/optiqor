package prwriter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/tenancy"
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

func TestPreview(t *testing.T) {
	const tenantID = "t1"
	happyLLM := func() *agent.FakeLLMClient {
		return &agent.FakeLLMClient{Responses: []agent.LLMResponse{{
			Text: "EXPLANATION:\nfix cpu\nDIFF:\n--- a\n+++ b\n@@\n-cpu: 2\n+cpu: 1\n",
		}}}
	}

	for _, tc := range []struct {
		name     string
		method   string
		tenant   string // empty means no tenant context
		body     any    // nil means http.NoBody
		llm      *agent.FakeLLMClient
		wantCode int
		check    func(t *testing.T, body []byte)
	}{
		{
			name:     "happy-path-returns-body-and-diff",
			method:   http.MethodPost,
			tenant:   tenantID,
			body:     mkPreviewReq(),
			llm:      happyLLM(),
			wantCode: http.StatusOK,
			check: func(t *testing.T, body []byte) {
				t.Helper()
				var resp PreviewResponse
				if err := json.Unmarshal(body, &resp); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(resp.MarkdownBody, "Optiqor analysis") {
					t.Errorf("markdown body missing header:\n%s", resp.MarkdownBody)
				}
				if !strings.Contains(resp.UnifiedDiff, "--- a") {
					t.Errorf("diff missing")
				}
			},
		},
		{
			name:     "no-tenant-returns-401",
			method:   http.MethodPost,
			tenant:   "",
			body:     mkPreviewReq(),
			llm:      &agent.FakeLLMClient{},
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "rejects-non-post",
			method:   http.MethodGet,
			tenant:   "",
			body:     nil,
			llm:      &agent.FakeLLMClient{},
			wantCode: http.StatusMethodNotAllowed,
		},
		{
			name:     "requires-chart-fields",
			method:   http.MethodPost,
			tenant:   tenantID,
			body:     PreviewRequest{},
			llm:      &agent.FakeLLMClient{},
			wantCode: http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{Composer: &agent.Composer{LLM: tc.llm}}

			var req *http.Request
			if tc.body == nil {
				req = httptest.NewRequest(tc.method, "/v1/apply-fixes", http.NoBody)
			} else {
				raw, err := json.Marshal(tc.body)
				if err != nil {
					t.Fatalf("marshal body: %v", err)
				}
				req = httptest.NewRequest(tc.method, "/v1/apply-fixes", strings.NewReader(string(raw)))
			}
			if tc.tenant != "" {
				req = req.WithContext(tenancy.WithContext(context.Background(), tenancy.Context{TenantID: tc.tenant}))
			}

			w := httptest.NewRecorder()
			h.Preview(w, req)

			if w.Code != tc.wantCode {
				t.Fatalf("code = %d, want %d; body = %s", w.Code, tc.wantCode, w.Body.String())
			}
			if tc.check != nil {
				tc.check(t, w.Body.Bytes())
			}
		})
	}
}
