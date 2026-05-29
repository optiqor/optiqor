package onboarding

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetHealth_UnauthorizedWithoutTenant(t *testing.T) {
	h := newTestHandler(time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC))
	req := httptest.NewRequest(http.MethodGet, "/v1/onboarding/health", http.NoBody)
	rec := httptest.NewRecorder()
	h.GetHealth(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", rec.Code)
	}
}

func TestGetHealth_FreshTenantSurfacesGitHubBlocker(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	h := newTestHandler(now)
	req := withTenant(httptest.NewRequest(http.MethodGet, "/v1/onboarding/health", http.NoBody))
	rec := httptest.NewRecorder()
	h.GetHealth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Current != StageSignedUp {
		t.Errorf("current = %s, want signed_up", got.Current)
	}
	if got.NextStage != StageVCSConnected {
		t.Errorf("next_stage = %s, want vcs_connected", got.NextStage)
	}
	if got.Activated {
		t.Error("fresh tenant must not report Activated=true")
	}
	if len(got.Blockers) != 1 || got.Blockers[0].Stage != StageSignedUp {
		t.Errorf("expected one signed_up blocker, got %+v", got.Blockers)
	}
	if got.TimeInStage == nil || got.TimeInStage.Seconds != 0 {
		t.Errorf("time_in_stage = %+v, want zero on first read", got.TimeInStage)
	}
	if got.SLOs.SandboxLatency == "" {
		t.Error("SLO table must be populated for the dashboard")
	}
}

func TestBuildHealthResponse_AdvancesAndReportsTimeInStage(t *testing.T) {
	start := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	st := New(start)
	if err := st.Advance(StageVCSConnected, start.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	resp := buildHealthResponse(st, start.Add(3*time.Hour))
	if resp.Current != StageVCSConnected {
		t.Errorf("current = %s, want vcs_connected", resp.Current)
	}
	if resp.TimeInStage == nil {
		t.Fatal("time_in_stage must populate after advance")
	}
	if resp.TimeInStage.Seconds != int64((1 * time.Hour).Seconds()) {
		t.Errorf("time_in_stage = %d, want 3600", resp.TimeInStage.Seconds)
	}
	if len(resp.Blockers) != 1 || resp.Blockers[0].Stage != StageVCSConnected {
		t.Errorf("blocker must follow current stage, got %+v", resp.Blockers)
	}
}

func TestBuildHealthResponse_TerminalStageHasNoBlockers(t *testing.T) {
	start := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	st := New(start)
	for _, s := range Stages[1:] {
		if err := st.Advance(s, start.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	resp := buildHealthResponse(st, start.Add(2*time.Hour))
	if resp.NextStage != "" {
		t.Errorf("next_stage at terminal must be empty, got %s", resp.NextStage)
	}
	if len(resp.Blockers) != 0 {
		t.Errorf("terminal stage must list zero blockers, got %+v", resp.Blockers)
	}
	if resp.TimeToReceipt == nil {
		t.Error("TimeToReceipt must populate once StageFirstReceipt is reached")
	}
}

func TestBuildHealthResponse_FutureNowClampsToZero(t *testing.T) {
	start := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	st := New(start)
	resp := buildHealthResponse(st, start.Add(-time.Hour))
	if resp.TimeInStage == nil || resp.TimeInStage.Seconds != 0 {
		t.Errorf("negative wall-clock should clamp; got %+v", resp.TimeInStage)
	}
}
