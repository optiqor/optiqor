package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lowplane/backend/internal/platform/healthz"
	"github.com/lowplane/backend/internal/tenancy"
)

// silentLogger is a slog logger that drops everything; used in every
// handler test so output stays clean.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealthz(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyz_AllOK(t *testing.T) {
	r := healthz.NewRegistry()
	r.Register("self", healthz.AlwaysOK)
	r.Register("more", healthz.AlwaysOK)

	mux := buildMux(r, silentLogger(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		OK     bool             `json:"ok"`
		Checks []healthz.Result `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, rec.Body.String())
	}
	if !body.OK || len(body.Checks) != 2 {
		t.Fatalf("body=%+v", body)
	}
}

func TestReadyz_Failing(t *testing.T) {
	r := healthz.NewRegistry()
	r.Register("self", healthz.AlwaysOK)
	r.Register("redis", healthz.AlwaysFail("connection refused"))

	mux := buildMux(r, silentLogger(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body struct {
		OK     bool             `json:"ok"`
		Checks []healthz.Result `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.OK {
		t.Fatal("body.ok should be false")
	}
	var found bool
	for _, c := range body.Checks {
		if c.Name == "redis" && !c.OK {
			found = true
		}
	}
	if !found {
		t.Errorf("redis-failure not surfaced: %+v", body.Checks)
	}
}

func TestRequestID_Generated(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil)
	rec := httptest.NewRecorder()
	withRequestID(mux).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("X-Request-ID should be generated when missing")
	}
}

func TestRequestID_Echoed(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	rec := httptest.NewRecorder()
	withRequestID(mux).ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); got != "abc-123" {
		t.Fatalf("X-Request-ID echoed = %q, want abc-123", got)
	}
}

func TestReadyz_TimeoutNotPanicking(t *testing.T) {
	r := healthz.NewRegistry()
	r.Register("slow", func(ctx context.Context) error {
		select {
		case <-time.After(time.Second):
			return nil
		case <-ctx.Done():
			return errors.New("ctx done")
		}
	})

	mux := buildMux(r, silentLogger(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Body.Len() == 0 {
		t.Fatal("body should not be empty")
	}
}

func TestGitHubWebhook_ValidSignature(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	secret := []byte("hush")

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	mux := buildMux(healthz.NewRegistry(), silentLogger(), secret)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "abc-123")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Status   string `json:"status"`
		Event    string `json:"event"`
		Delivery string `json:"delivery"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "accepted" || got.Event != "pull_request" || got.Delivery != "abc-123" {
		t.Fatalf("body=%+v", got)
	}
}

func TestGitHubWebhook_TamperedRejected(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	secret := []byte("hush")

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	mux := buildMux(healthz.NewRegistry(), silentLogger(), secret)
	tampered := []byte(`{"action":"closed"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(tampered))
	req.Header.Set("X-Hub-Signature-256", signature)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestGitHubWebhook_DevModeAcceptsUnsigned(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil)
	body := []byte(`{"action":"opened"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("dev mode (no secret) should accept unsigned; got %d", rec.Code)
	}
}

func TestHeaderTenantExtractor_Valid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Sevro-Tenant", "tenant-1")
	req.Header.Set("X-Sevro-Workspace", "ws-1")
	t1, err := HeaderTenantExtractor(req)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if t1.TenantID != "tenant-1" || t1.WorkspaceID != "ws-1" {
		t.Errorf("extractor lost values: %+v", t1)
	}
}

func TestHeaderTenantExtractor_Empty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if _, err := HeaderTenantExtractor(req); !errors.Is(err, tenancy.ErrNoTenant) {
		t.Fatalf("expected ErrNoTenant, got %v", err)
	}
}

func TestRequireTenant_Allows(t *testing.T) {
	called := false
	h := requireTenant(HeaderTenantExtractor, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t1, err := tenancy.FromContext(r.Context())
		if err != nil {
			t.Fatalf("inner: %v", err)
		}
		if t1.TenantID != "t1" {
			t.Errorf("tenant id mismatch: %v", t1)
		}
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Sevro-Tenant", "t1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("inner handler should be reached")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRequireTenant_Rejects(t *testing.T) {
	h := requireTenant(HeaderTenantExtractor, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("inner handler should NOT be reached")
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil) // no tenant header
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
