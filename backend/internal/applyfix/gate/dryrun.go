package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// DryrunValidator calls the agent's signed-token /v1/dry-run endpoint
// from the SaaS side, asking the in-cluster agent to render the patched
// chart and run `kubectl --dry-run=server` against the cluster's real
// admission webhooks (Kyverno / Gatekeeper / OPA). Without an agent
// the validator can't reach the cluster — wire AgentClient at boot
// only after Phase-5 onboarding has installed the agent.
type DryrunValidator struct {
	Client AgentClient
}

func (DryrunValidator) Stage() Stage { return StageDryrun }

func (v DryrunValidator) Validate(ctx context.Context, t tenancy.Context, c Candidate) StageResult {
	if v.Client == nil {
		return StageResult{
			Stage: StageDryrun, Status: StatusNotImplemented,
			Detail: "no agent client configured",
			Err:    ErrNotImplemented,
		}
	}
	out, err := v.Client.DryRun(ctx, t, DryRunRequest(c))
	if err != nil {
		return failed(StageDryrun, "agent dry-run errored", err)
	}
	if !out.Accepted {
		return failed(StageDryrun, "agent rejected dry-run: "+out.Detail, errors.New("gate/dryrun: agent rejected"))
	}
	return StageResult{Stage: StageDryrun, Status: StatusPassed}
}

// AgentClient is the seam to the agent's dry-run service. Real
// implementation lives in internal/agent/client; tests use a fake.
type AgentClient interface {
	DryRun(ctx context.Context, t tenancy.Context, req DryRunRequest) (DryRunResponse, error)
}

type DryRunRequest struct {
	ApplyFixID  string `json:"apply_fix_id"`
	ChartYAML   string `json:"chart_yaml"`
	UnifiedDiff string `json:"unified_diff"`
	Workload    string `json:"workload"`
}

type DryRunResponse struct {
	Accepted bool   `json:"accepted"`
	Detail   string `json:"detail,omitempty"`
}

// HTTPAgentClient calls the agent over signed-token mTLS HTTPS. The
// signed-token round-trip (Phase 5) plugs in via TokenProvider; until
// then NewHTTPAgentClient uses a static bearer for dev.
type HTTPAgentClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
	Timeout    time.Duration
}

func NewHTTPAgentClient(baseURL, token string) *HTTPAgentClient {
	return &HTTPAgentClient{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		Token:      token,
		Timeout:    10 * time.Second,
	}
}

func (c *HTTPAgentClient) DryRun(ctx context.Context, t tenancy.Context, req DryRunRequest) (DryRunResponse, error) {
	if c.BaseURL == "" {
		return DryRunResponse{}, errors.New("gate/dryrun: HTTPAgentClient BaseURL not set")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return DryRunResponse{}, fmt.Errorf("gate/dryrun: marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/dry-run", bytes.NewReader(body))
	if err != nil {
		return DryRunResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Optiqor-Tenant", t.TenantID)
	if c.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return DryRunResponse{}, fmt.Errorf("gate/dryrun: agent unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(resp.Body)
		return DryRunResponse{}, fmt.Errorf("gate/dryrun: agent returned %d: %s", resp.StatusCode, raw)
	}
	var out DryRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return DryRunResponse{}, fmt.Errorf("gate/dryrun: decode response: %w", err)
	}
	return out, nil
}
