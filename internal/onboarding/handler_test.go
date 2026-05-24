package onboarding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

const testTenantID = "tenant-1"

func withTenant(req *http.Request) *http.Request {
	return req.WithContext(tenancy.WithContext(req.Context(), tenancy.Context{TenantID: testTenantID}))
}

func newTestHandler(now time.Time) *Handler {
	svc := NewService(NewInMemoryStore())
	svc.Now = func() time.Time { return now }
	return &Handler{Service: svc}
}

func TestGetState_AutoCreatesOnFirstRead(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	h := newTestHandler(now)
	req := withTenant(httptest.NewRequest(http.MethodGet, "/v1/onboarding/state", http.NoBody))
	rec := httptest.NewRecorder()
	h.GetState(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var got StateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Current != StageSignedUp {
		t.Errorf("current: got %q want %q", got.Current, StageSignedUp)
	}
	if got.ProgressPercent != 0 {
		t.Errorf("progress at signup: got %d want 0", got.ProgressPercent)
	}
	if got.NextStage != StageVCSConnected {
		t.Errorf("next_stage: got %q want %q", got.NextStage, StageVCSConnected)
	}
	if got.SLOs.InstallToFirstReceipt == "" {
		t.Error("SLOs must be surfaced for the dashboard timeline")
	}
}

func TestGetState_401WithoutTenant(t *testing.T) {
	h := newTestHandler(time.Now())
	rec := httptest.NewRecorder()
	h.GetState(rec, httptest.NewRequest(http.MethodGet, "/v1/onboarding/state", http.NoBody))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rec.Code)
	}
}

func TestTransition_AdvancesForward(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	h := newTestHandler(now)
	req := withTenant(httptest.NewRequest(http.MethodPost, "/v1/onboarding/transition",
		strings.NewReader(`{"to":"vcs_connected"}`)))
	rec := httptest.NewRecorder()
	h.Transition(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var got StateResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Current != StageVCSConnected {
		t.Errorf("current: got %q want vcs_connected", got.Current)
	}
	if _, ok := got.ReachedAt[StageVCSConnected]; !ok {
		t.Error("reached_at must include the new stage's timestamp")
	}
}

func TestTransition_RejectsBackwards(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	h := newTestHandler(now)

	// Advance forward first.
	advance := withTenant(httptest.NewRequest(http.MethodPost, "/v1/onboarding/transition",
		strings.NewReader(`{"to":"agent_installed"}`)))
	h.Transition(httptest.NewRecorder(), advance)

	// Try to go backwards.
	rec := httptest.NewRecorder()
	back := withTenant(httptest.NewRequest(http.MethodPost, "/v1/onboarding/transition",
		strings.NewReader(`{"to":"signed_up"}`)))
	h.Transition(rec, back)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("backwards transition: want 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTransition_RejectsUnknownStage(t *testing.T) {
	h := newTestHandler(time.Now())
	req := withTenant(httptest.NewRequest(http.MethodPost, "/v1/onboarding/transition",
		strings.NewReader(`{"to":"warp-drive"}`)))
	rec := httptest.NewRecorder()
	h.Transition(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown stage: want 400, got %d", rec.Code)
	}
}

func TestTransition_RejectsUnknownFields(t *testing.T) {
	h := newTestHandler(time.Now())
	req := withTenant(httptest.NewRequest(http.MethodPost, "/v1/onboarding/transition",
		strings.NewReader(`{"to":"vcs_connected","extra":"nope"}`)))
	rec := httptest.NewRecorder()
	h.Transition(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: want 400, got %d", rec.Code)
	}
}

func TestService_GetIsIdempotent(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	svc := NewService(NewInMemoryStore())
	svc.Now = func() time.Time { return now }

	first, err := svc.Get(context.Background(), "tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Get(context.Background(), "tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Reached[StageSignedUp] != second.Reached[StageSignedUp] {
		t.Errorf("repeated Get must not bump signup timestamp: %v vs %v",
			first.Reached[StageSignedUp], second.Reached[StageSignedUp])
	}
}
