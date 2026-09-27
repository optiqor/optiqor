//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/applyfix/gate"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// TestDryrunValidator_AgentRoundtrip stands up a fake agent HTTP
// service and runs a full DryrunValidator → HTTPAgentClient call.
// Pins the wire shape Phase-5 onboarding will plug into.
func TestDryrunValidator_AgentRoundtrip(t *testing.T) {
	var captured gate.DryRunRequest
	var seenTenant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		seenTenant = r.Header.Get("X-Optiqor-Tenant")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gate.DryRunResponse{Accepted: true, Detail: "kyverno: clean"})
	}))
	defer srv.Close()

	v := gate.DryrunValidator{Client: gate.NewHTTPAgentClient(srv.URL, "tenant-token-stub")}
	res := v.Validate(context.Background(), tenancy.Context{TenantID: "tenant-roundtrip"}, gate.Candidate{
		ApplyFixID:  "afix-int-1",
		ChartYAML:   "api:\n  replicas: 3",
		UnifiedDiff: "diff body",
		Workload:    "api",
	})
	if res.Status != gate.StatusPassed {
		t.Fatalf("status=%s detail=%q err=%v", res.Status, res.Detail, res.Err)
	}
	if captured.ApplyFixID != "afix-int-1" {
		t.Errorf("ApplyFixID round-trip = %q", captured.ApplyFixID)
	}
	if captured.Workload != "api" {
		t.Errorf("Workload round-trip = %q", captured.Workload)
	}
	if seenTenant != "tenant-roundtrip" {
		t.Errorf("X-Optiqor-Tenant header = %q", seenTenant)
	}
}

// TestDryrunValidator_AgentRejectsAdmission asserts that when the
// agent rejects (admission webhook etc.), the gate fails closed.
func TestDryrunValidator_AgentRejectsAdmission(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(gate.DryRunResponse{Accepted: false, Detail: "kyverno: privileged container blocked"})
	}))
	defer srv.Close()

	v := gate.DryrunValidator{Client: gate.NewHTTPAgentClient(srv.URL, "t")}
	res := v.Validate(context.Background(), tenancy.Context{TenantID: "t"}, gate.Candidate{
		ApplyFixID: "a", ChartYAML: "x: 1", UnifiedDiff: "d", Workload: "w",
	})
	if res.Status != gate.StatusFailed {
		t.Fatalf("status=%s, want failed", res.Status)
	}
	if !strings.Contains(res.Detail, "kyverno") {
		t.Errorf("detail %q should include agent rejection reason", res.Detail)
	}
}
