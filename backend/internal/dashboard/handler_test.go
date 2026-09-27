package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

type fakeSavings struct {
	resp SavingsSummary
	err  error
}

func (f *fakeSavings) Summary(_ tenancy.Context) (SavingsSummary, error) { return f.resp, f.err }

type fakeApplyFixes struct {
	items []ApplyFix
	err   error
}

func (f *fakeApplyFixes) List(_ tenancy.Context, _ string, _ int) ([]ApplyFix, error) {
	return f.items, f.err
}

type fakeAgent struct {
	resp AgentHealth
	err  error
}

func (f *fakeAgent) Latest(_ tenancy.Context) (AgentHealth, error) { return f.resp, f.err }

func withTenant(req *http.Request) *http.Request {
	return req.WithContext(tenancy.WithContext(context.Background(), tenancy.Context{TenantID: "tenant-uuid"}))
}

func TestSummary_ReturnsSavingsJSON(t *testing.T) {
	h := &Handler{Savings: &fakeSavings{resp: SavingsSummary{LifetimeCents: 50_000, MTDCents: 2920, MergedCount: 2}}}
	req := withTenant(httptest.NewRequest(http.MethodGet, "/v1/savings/summary", http.NoBody))
	rec := httptest.NewRecorder()
	h.Summary(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got SavingsSummary
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.LifetimeCents != 50_000 || got.MTDCents != 2920 {
		t.Errorf("body = %+v", got)
	}
}

func TestSummary_NoTenant_Unauthorized(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/v1/savings/summary", http.NoBody)
	rec := httptest.NewRecorder()
	h.Summary(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSummary_SourceError_500(t *testing.T) {
	h := &Handler{Savings: &fakeSavings{err: errors.New("db down")}}
	req := withTenant(httptest.NewRequest(http.MethodGet, "/v1/savings/summary", http.NoBody))
	rec := httptest.NewRecorder()
	h.Summary(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestListApplyFixes_DefaultsToOpenState(t *testing.T) {
	src := &fakeApplyFixes{items: []ApplyFix{
		{ID: "af-1", Repo: "acme/api", State: "open", MonthlyUSDCents: 2920},
	}}
	h := &Handler{ApplyFixes: src}
	req := withTenant(httptest.NewRequest(http.MethodGet, "/v1/apply-fixes", http.NoBody))
	rec := httptest.NewRecorder()
	h.ListApplyFixes(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got applyFixesPage
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Items) != 1 || got.Items[0].Repo != "acme/api" {
		t.Errorf("body = %+v", got)
	}
}

func TestListApplyFixes_InvalidState_400(t *testing.T) {
	h := &Handler{ApplyFixes: &fakeApplyFixes{}}
	req := withTenant(httptest.NewRequest(http.MethodGet, "/v1/apply-fixes?state=bogus", http.NoBody))
	rec := httptest.NewRecorder()
	h.ListApplyFixes(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "allowed") {
		t.Errorf("missing allowed-states hint: %s", rec.Body.String())
	}
}

func TestListApplyFixes_EmptyResult_EmptyArray(t *testing.T) {
	h := &Handler{ApplyFixes: &fakeApplyFixes{}}
	req := withTenant(httptest.NewRequest(http.MethodGet, "/v1/apply-fixes", http.NoBody))
	rec := httptest.NewRecorder()
	h.ListApplyFixes(rec, req)
	if !strings.Contains(rec.Body.String(), "\"items\":[]") {
		t.Errorf("body must be {\"items\":[]} when empty: %s", rec.Body.String())
	}
}

func TestAgentHealth_NoSource_ReturnsOffline(t *testing.T) {
	h := &Handler{}
	req := withTenant(httptest.NewRequest(http.MethodGet, "/v1/agent/health", http.NoBody))
	rec := httptest.NewRecorder()
	h.AgentHealth(rec, req)
	var got AgentHealth
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != "offline" {
		t.Errorf("status = %q", got.Status)
	}
}

func TestAgentHealth_HappyPath(t *testing.T) {
	now := time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC)
	h := &Handler{Agent: &fakeAgent{resp: AgentHealth{Status: "healthy", LastCheckin: now, DataFreshnessSeconds: 30, Version: "v0.1"}}}
	req := withTenant(httptest.NewRequest(http.MethodGet, "/v1/agent/health", http.NoBody))
	rec := httptest.NewRecorder()
	h.AgentHealth(rec, req)
	var got AgentHealth
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Status != "healthy" || got.Version != "v0.1" {
		t.Errorf("body = %+v", got)
	}
}
