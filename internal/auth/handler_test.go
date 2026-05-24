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

func TestWhoami_FromBearerToken(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	h, signer := newHandler(t, now)
	token, _ := signer.Issue(validSession(now))

	req := httptest.NewRequest(http.MethodGet, "/v1/session/whoami", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.Whoami(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var got WhoamiResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "tenant-1" || got.Subject != "alice@example.test" {
		t.Errorf("claim mapping wrong: %+v", got)
	}
	if got.Source != "jwt" {
		t.Errorf("source: got %q want jwt", got.Source)
	}
	if got.ExpiresAt == "" {
		t.Error("expires_at must be surfaced when source=jwt")
	}
}

func TestWhoami_FromCookie(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	h, signer := newHandler(t, now)
	token, _ := signer.Issue(validSession(now))

	req := httptest.NewRequest(http.MethodGet, "/v1/session/whoami", http.NoBody)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	rec := httptest.NewRecorder()
	h.Whoami(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
}

func TestWhoami_FallsBackToTenantHeaderContext(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	h, _ := newHandler(t, now)

	req := httptest.NewRequest(http.MethodGet, "/v1/session/whoami", http.NoBody)
	ctx := tenancy.WithContext(req.Context(), tenancy.Context{TenantID: "tenant-2", WorkspaceID: "ws-2"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.Whoami(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var got WhoamiResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Source != "header" || got.TenantID != "tenant-2" {
		t.Errorf("header fallback: %+v", got)
	}
}

func TestWhoami_401WhenNoIdentity(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	h, _ := newHandler(t, now)
	rec := httptest.NewRecorder()
	h.Whoami(rec, httptest.NewRequest(http.MethodGet, "/v1/session/whoami", http.NoBody))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rec.Code)
	}
}

func TestWhoami_ExpiredCookieFallsThroughCleanly(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	h, signer := newHandler(t, now)
	token, _ := signer.Issue(validSession(now))
	// Advance the clock past the expiry so Verify fails.
	signer.Now = func() time.Time { return now.Add(2 * time.Hour) }

	req := httptest.NewRequest(http.MethodGet, "/v1/session/whoami", http.NoBody)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	rec := httptest.NewRecorder()
	h.Whoami(rec, req)
	// No tenant header, no valid JWT → must fail closed with 401, not crash.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expired cookie + no header should 401, got %d", rec.Code)
	}
}

func TestIssue_HappyPath(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	h, _ := newHandler(t, now)

	body := strings.NewReader(`{"subject":"bob@example.test","name":"Bob","tenant_id":"tenant-3","workspace_id":"ws-3"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/session/issue", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Issue(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
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
}

func TestIssue_RejectsMissingFields(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	h, _ := newHandler(t, now)
	for _, body := range []string{
		`{}`,
		`{"subject":"alice"}`,
		`{"tenant_id":"t"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/session/issue", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.Issue(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: want 400, got %d", body, rec.Code)
		}
	}
}

func TestIssue_RejectsUnknownFields(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	h, _ := newHandler(t, now)
	body := strings.NewReader(`{"subject":"a","tenant_id":"t","extra":"nope"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/session/issue", body)
	rec := httptest.NewRecorder()
	h.Issue(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("want 400 on unknown field, got %d", rec.Code)
	}
}
