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
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/platform/healthz"
	"github.com/optiqor/optiqor/internal/platform/telemetry"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// silentLogger drops every record so handler tests stay quiet.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealthz(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyz_AllOK(t *testing.T) {
	r := healthz.NewRegistry()
	r.Register("self", healthz.AlwaysOK)
	r.Register("more", healthz.AlwaysOK)

	mux := buildMux(r, silentLogger(), nil, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody))

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

	mux := buildMux(r, silentLogger(), nil, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody))

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
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, nil)
	rec := httptest.NewRecorder()
	withRequestID(mux).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))
	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("X-Request-ID should be generated when missing")
	}
}

func TestRequestID_Echoed(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
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

	mux := buildMux(r, silentLogger(), nil, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody))
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

	mux := buildMux(healthz.NewRegistry(), silentLogger(), secret, nil)
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

	mux := buildMux(healthz.NewRegistry(), silentLogger(), secret, nil)
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
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, nil)
	body := []byte(`{"action":"opened"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("dev mode (no secret) should accept unsigned; got %d", rec.Code)
	}
}

func TestHeaderTenantExtractor_Valid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	req.Header.Set("X-Optiqor-Tenant", "tenant-1")
	req.Header.Set("X-Optiqor-Workspace", "ws-1")
	t1, err := HeaderTenantExtractor(req)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if t1.TenantID != "tenant-1" || t1.WorkspaceID != "ws-1" {
		t.Errorf("extractor lost values: %+v", t1)
	}
}

func TestHeaderTenantExtractor_Empty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
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
	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	req.Header.Set("X-Optiqor-Tenant", "t1")
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
	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMetrics_ExposesRegistry(t *testing.T) {
	reg := telemetry.NewRegistry()
	c := reg.NewCounter("optiqor_test_total", "test counter", nil)
	c.Add(7)

	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, reg)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "optiqor_test_total 7") {
		t.Errorf("metrics body missing counter:\n%s", rec.Body.String())
	}
}

func TestPanicRecovery_Returns500AndDoesNotPropagate(t *testing.T) {
	panicker := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("kaboom")
	})
	wrapped := withPanicRecovery(silentLogger(), panicker)

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", http.NoBody))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "internal server error") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestPanicRecovery_PassesThroughWhenNoPanic(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hi"))
	})
	wrapped := withPanicRecovery(silentLogger(), ok)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", http.NoBody))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", rec.Code)
	}
	if rec.Body.String() != "hi" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestAccessLog_RecordsCounterAndLatency(t *testing.T) {
	reg := telemetry.NewRegistry()
	requests := reg.NewCounter("optiqor_http_requests_total", "", nil)
	latency := reg.NewHistogram("optiqor_http_request_duration_seconds", "", nil, []float64{0.1, 1})

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	wrapped := withAccessLog(silentLogger(), requests, latency, inner)

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", http.NoBody))

	if got := requests.Value(); got != 1 {
		t.Errorf("requests counter = %v, want 1", got)
	}
	if got := latency.Snapshot().Count; got != 1 {
		t.Errorf("latency histogram count = %d, want 1", got)
	}
}

func TestRecordingWriter_StatusDefaultsTo200OnImplicitWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &recordingWriter{ResponseWriter: rec, status: 200}
	if _, err := rw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if rw.status != 200 {
		t.Errorf("status = %d", rw.status)
	}
	if rw.bytes != 5 {
		t.Errorf("bytes = %d", rw.bytes)
	}
}

func TestRecordingWriter_DoesNotDoubleWriteHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &recordingWriter{ResponseWriter: rec, status: 200}
	rw.WriteHeader(http.StatusBadRequest)
	rw.WriteHeader(http.StatusInternalServerError)
	if rw.status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.status)
	}
}

func TestGitHubOAuthCallback_RequiresCode(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oauth/github/callback", http.NoBody))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGitHubOAuthCallback_AcceptsCode(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth/github/callback?state=abc&code=xyz", http.NoBody)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"phase":"1"`) {
		t.Errorf("body missing phase ack: %s", rec.Body.String())
	}
}

func TestPProf_GatedByAdminToken(t *testing.T) {
	mux := http.NewServeMux()
	mountPProf(mux, "secret-token")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/debug/pprof/", http.NoBody))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: status = %d, want 401", rec.Code)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", http.NoBody)
	req.Header.Set("X-Admin-Token", "wrong")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want 401", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/debug/pprof/", http.NoBody)
	req.Header.Set("X-Admin-Token", "secret-token")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct token: status = %d, want 200", rec.Code)
	}
}

func TestSubtleConstantTimeEq(t *testing.T) {
	cases := []struct {
		a, b string
		eq   int
	}{
		{"abc", "abc", 1},
		{"abc", "abd", 0},
		{"abc", "ab", 0},
		{"", "", 1},
	}
	for _, tc := range cases {
		if got := subtleConstantTimeEq(tc.a, tc.b); got != tc.eq {
			t.Errorf("eq(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.eq)
		}
	}
}
