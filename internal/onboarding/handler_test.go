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

func TestGetState(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name       string
		req        func() *http.Request
		wantStatus int
		check      func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name: "auto creates on first read",
			req: func() *http.Request {
				return withTenant(httptest.NewRequest(http.MethodGet, "/v1/onboarding/state", http.NoBody))
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
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
			},
		},
		{
			name: "401 without tenant",
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "/v1/onboarding/state", http.NoBody)
			},
			wantStatus: http.StatusUnauthorized,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(now)
			rec := httptest.NewRecorder()
			h.GetState(rec, tc.req())
			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d want %d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.check != nil {
				tc.check(t, rec)
			}
		})
	}
}

func TestTransition(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name       string
		setup      func(t *testing.T, h *Handler)
		body       string
		wantStatus int
		check      func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:       "advances forward",
			body:       `{"to":"vcs_connected"}`,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var got StateResponse
				_ = json.Unmarshal(rec.Body.Bytes(), &got)
				if got.Current != StageVCSConnected {
					t.Errorf("current: got %q want vcs_connected", got.Current)
				}
				if _, ok := got.ReachedAt[StageVCSConnected]; !ok {
					t.Error("reached_at must include the new stage's timestamp")
				}
			},
		},
		{
			name: "rejects backwards",
			setup: func(t *testing.T, h *Handler) {
				t.Helper()
				adv := withTenant(httptest.NewRequest(http.MethodPost, "/v1/onboarding/transition",
					strings.NewReader(`{"to":"agent_installed"}`)))
				h.Transition(httptest.NewRecorder(), adv)
			},
			body:       `{"to":"signed_up"}`,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "rejects unknown stage",
			body:       `{"to":"warp-drive"}`,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "rejects unknown field",
			body:       `{"to":"vcs_connected","extra":"nope"}`,
			wantStatus: http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(now)
			if tc.setup != nil {
				tc.setup(t, h)
			}
			req := withTenant(httptest.NewRequest(http.MethodPost, "/v1/onboarding/transition",
				strings.NewReader(tc.body)))
			rec := httptest.NewRecorder()
			h.Transition(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d want %d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.check != nil {
				tc.check(t, rec)
			}
		})
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
