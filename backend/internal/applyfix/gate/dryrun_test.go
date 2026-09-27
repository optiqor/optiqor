package gate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestDryrunValidator_Validate(t *testing.T) {
	ctx := context.Background()
	tnt := tenancy.Context{TenantID: "t-dr"}

	for _, tc := range []struct {
		name     string
		client   AgentClient
		wantStat Status
		errSub   string
	}{
		{
			name:     "nil client is not_implemented",
			client:   nil,
			wantStat: StatusNotImplemented,
		},
		{
			name:     "agent accepts -> passed",
			client:   stubAgent{resp: DryRunResponse{Accepted: true}},
			wantStat: StatusPassed,
		},
		{
			name:     "agent rejects -> failed",
			client:   stubAgent{resp: DryRunResponse{Accepted: false, Detail: "kyverno: privileged container blocked"}},
			wantStat: StatusFailed,
			errSub:   "kyverno",
		},
		{
			name:     "agent error -> failed",
			client:   stubAgent{err: errors.New("agent unreachable")},
			wantStat: StatusFailed,
			errSub:   "errored",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := DryrunValidator{Client: tc.client}
			got := v.Validate(ctx, tnt, Candidate{ApplyFixID: "af-1", ChartYAML: "x: 1\n", UnifiedDiff: "diff", Workload: "api"})
			if got.Status != tc.wantStat {
				t.Fatalf("status=%s want %s (detail=%q)", got.Status, tc.wantStat, got.Detail)
			}
			if tc.errSub != "" && !strings.Contains(got.Detail, tc.errSub) {
				t.Errorf("detail %q missing %q", got.Detail, tc.errSub)
			}
		})
	}
}

func TestHTTPAgentClient_DryRun_HappyPath(t *testing.T) {
	var got DryRunRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/dry-run" {
			t.Errorf("path = %q, want /v1/dry-run", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing bearer token: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Optiqor-Tenant") != "t-dr" {
			t.Errorf("missing tenant header: %q", r.Header.Get("X-Optiqor-Tenant"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DryRunResponse{Accepted: true})
	}))
	defer srv.Close()

	c := NewHTTPAgentClient(srv.URL, "test-token")
	resp, err := c.DryRun(context.Background(), tenancy.Context{TenantID: "t-dr"}, DryRunRequest{
		ApplyFixID:  "af-1",
		ChartYAML:   "api:",
		UnifiedDiff: "diff",
		Workload:    "api",
	})
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if !resp.Accepted {
		t.Errorf("Accepted = false, want true")
	}
	if got.ApplyFixID != "af-1" {
		t.Errorf("ApplyFixID round-trip lost: %+v", got)
	}
}

func TestHTTPAgentClient_DryRun_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewHTTPAgentClient(srv.URL, "test-token")
	_, err := c.DryRun(context.Background(), tenancy.Context{TenantID: "t-dr"}, DryRunRequest{})
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("err %q should include 403", err.Error())
	}
}

func TestHTTPAgentClient_DryRun_EmptyBaseURL(t *testing.T) {
	c := NewHTTPAgentClient("", "")
	_, err := c.DryRun(context.Background(), tenancy.Context{TenantID: "t-dr"}, DryRunRequest{})
	if err == nil || !strings.Contains(err.Error(), "BaseURL") {
		t.Errorf("err = %v, want substring BaseURL", err)
	}
}

type stubAgent struct {
	resp DryRunResponse
	err  error
}

func (s stubAgent) DryRun(_ context.Context, _ tenancy.Context, _ DryRunRequest) (DryRunResponse, error) {
	return s.resp, s.err
}
