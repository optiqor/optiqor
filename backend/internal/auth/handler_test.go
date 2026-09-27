package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func newHandler(t *testing.T, now time.Time) (*Handler, *Signer) {
	t.Helper()
	s := newSigner(t, now)
	return &Handler{Signer: s}, s
}

func TestWhoami(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name       string
		prepare    func(t *testing.T, h *Handler, s *Signer) *http.Request
		wantStatus int
		wantSource string // "" means don't check
		wantTenant string
	}{
		{
			name: "bearer token",
			prepare: func(t *testing.T, h *Handler, s *Signer) *http.Request {
				t.Helper()
				tok, _ := s.Issue(validSession(now))
				r := httptest.NewRequest(http.MethodGet, "/v1/session/whoami", http.NoBody)
				r.Header.Set("Authorization", "Bearer "+tok)
				return r
			},
			wantStatus: http.StatusOK,
			wantSource: "jwt",
			wantTenant: "tenant-1",
		},
		{
			name: "cookie",
			prepare: func(t *testing.T, h *Handler, s *Signer) *http.Request {
				t.Helper()
				tok, _ := s.Issue(validSession(now))
				r := httptest.NewRequest(http.MethodGet, "/v1/session/whoami", http.NoBody)
				r.AddCookie(&http.Cookie{Name: CookieName, Value: tok})
				return r
			},
			wantStatus: http.StatusOK,
			wantSource: "jwt",
			wantTenant: "tenant-1",
		},
		{
			name: "tenant header fallback",
			prepare: func(t *testing.T, _ *Handler, _ *Signer) *http.Request {
				t.Helper()
				r := httptest.NewRequest(http.MethodGet, "/v1/session/whoami", http.NoBody)
				ctx := tenancy.WithContext(r.Context(), tenancy.Context{TenantID: "tenant-2", WorkspaceID: "ws-2"})
				return r.WithContext(ctx)
			},
			wantStatus: http.StatusOK,
			wantSource: "header",
			wantTenant: "tenant-2",
		},
		{
			name: "no identity",
			prepare: func(t *testing.T, _ *Handler, _ *Signer) *http.Request {
				t.Helper()
				return httptest.NewRequest(http.MethodGet, "/v1/session/whoami", http.NoBody)
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			// Expired cookie + no header must fail closed with 401, not
			// crash and not 500.
			name: "expired cookie falls through",
			prepare: func(t *testing.T, h *Handler, s *Signer) *http.Request {
				t.Helper()
				tok, _ := s.Issue(validSession(now))
				s.Now = func() time.Time { return now.Add(2 * time.Hour) }
				r := httptest.NewRequest(http.MethodGet, "/v1/session/whoami", http.NoBody)
				r.AddCookie(&http.Cookie{Name: CookieName, Value: tok})
				return r
			},
			wantStatus: http.StatusUnauthorized,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, s := newHandler(t, now)
			req := tc.prepare(t, h, s)
			rec := httptest.NewRecorder()
			h.Whoami(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d want %d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var got WhoamiResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if tc.wantSource != "" && got.Source != tc.wantSource {
				t.Errorf("source: got %q want %q", got.Source, tc.wantSource)
			}
			if tc.wantTenant != "" && got.TenantID != tc.wantTenant {
				t.Errorf("tenant: got %q want %q", got.TenantID, tc.wantTenant)
			}
			if got.Source == "jwt" && got.ExpiresAt == "" {
				t.Error("expires_at must be surfaced when source=jwt")
			}
		})
	}
}

func TestIssue(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name       string
		body       string
		wantStatus int
		check      func(t *testing.T, rec *httptest.ResponseRecorder)
	}{
		{
			name:       "happy path",
			body:       `{"subject":"bob@example.test","name":"Bob","tenant_id":"tenant-3","workspace_id":"ws-3"}`,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, rec *httptest.ResponseRecorder) {
				t.Helper()
				var got IssueResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.Token == "" || got.ExpiresAt == "" {
					t.Errorf("response missing token/expires_at: %+v", got)
				}
				cookies := rec.Result().Cookies()
				if len(cookies) == 0 || cookies[0].Name != CookieName {
					t.Fatalf("issue must set the %q cookie", CookieName)
				}
				if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
					t.Errorf("cookie security: got %+v", cookies[0])
				}
			},
		},
		{name: "empty body", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "missing tenant", body: `{"subject":"alice"}`, wantStatus: http.StatusBadRequest},
		{name: "missing subject", body: `{"tenant_id":"t"}`, wantStatus: http.StatusBadRequest},
		{name: "unknown field", body: `{"subject":"a","tenant_id":"t","extra":"nope"}`, wantStatus: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newHandler(t, now)
			req := httptest.NewRequest(http.MethodPost, "/v1/session/issue", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.Issue(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d want %d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.check != nil {
				tc.check(t, rec)
			}
		})
	}
}
