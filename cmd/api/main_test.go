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

func TestHealthz_OK(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyz(t *testing.T) {
	slowCheck := func(ctx context.Context) error {
		select {
		case <-time.After(time.Second):
			return nil
		case <-ctx.Done():
			return errors.New("ctx done")
		}
	}
	for _, tc := range []struct {
		name      string
		setup     func(*healthz.Registry)
		wantCode  int
		checkBody func(t *testing.T, body []byte)
	}{
		{
			name: "all ok",
			setup: func(r *healthz.Registry) {
				r.Register("self", healthz.AlwaysOK)
				r.Register("more", healthz.AlwaysOK)
			},
			wantCode: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				t.Helper()
				var got struct {
					OK     bool             `json:"ok"`
					Checks []healthz.Result `json:"checks"`
				}
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("decode: %v\nbody=%s", err, body)
				}
				if !got.OK || len(got.Checks) != 2 {
					t.Fatalf("body=%+v", got)
				}
			},
		},
		{
			name: "failing check surfaces 503",
			setup: func(r *healthz.Registry) {
				r.Register("self", healthz.AlwaysOK)
				r.Register("redis", healthz.AlwaysFail("connection refused"))
			},
			wantCode: http.StatusServiceUnavailable,
			checkBody: func(t *testing.T, body []byte) {
				t.Helper()
				var got struct {
					OK     bool             `json:"ok"`
					Checks []healthz.Result `json:"checks"`
				}
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if got.OK {
					t.Fatal("body.ok should be false")
				}
				var found bool
				for _, c := range got.Checks {
					if c.Name == "redis" && !c.OK {
						found = true
					}
				}
				if !found {
					t.Errorf("redis-failure not surfaced: %+v", got.Checks)
				}
			},
		},
		{
			name: "slow check times out without panic",
			setup: func(r *healthz.Registry) {
				r.Register("slow", slowCheck)
			},
			checkBody: func(t *testing.T, body []byte) {
				t.Helper()
				if len(body) == 0 {
					t.Fatal("body should not be empty")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := healthz.NewRegistry()
			tc.setup(r)
			mux := buildMux(r, silentLogger(), nil, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody))
			if tc.wantCode != 0 && rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if tc.checkBody != nil {
				tc.checkBody(t, rec.Body.Bytes())
			}
		})
	}
}

func TestRequestID(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    string // empty means "any non-empty"
	}{
		{name: "generated when missing"},
		{name: "echoed when present", headers: map[string]string{"X-Request-ID": "abc-123"}, want: "abc-123"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			withRequestID(mux).ServeHTTP(rec, req)
			got := rec.Header().Get("X-Request-ID")
			if tc.want == "" {
				if got == "" {
					t.Fatal("X-Request-ID should be generated when missing")
				}
				return
			}
			if got != tc.want {
				t.Fatalf("X-Request-ID = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGitHubWebhook(t *testing.T) {
	signedBody := []byte(`{"action":"opened"}`)
	secret := []byte("hush")
	mac := hmac.New(sha256.New, secret)
	mac.Write(signedBody)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	for _, tc := range []struct {
		name     string
		secret   []byte
		body     []byte
		headers  map[string]string
		wantCode int
		checkOK  func(t *testing.T, body []byte)
	}{
		{
			name:   "valid signature accepted",
			secret: secret,
			body:   signedBody,
			headers: map[string]string{
				"X-Hub-Signature-256": validSig,
				"X-GitHub-Event":      "pull_request",
				"X-GitHub-Delivery":   "abc-123",
			},
			wantCode: http.StatusAccepted,
			checkOK: func(t *testing.T, body []byte) {
				t.Helper()
				var got struct {
					Status   string `json:"status"`
					Event    string `json:"event"`
					Delivery string `json:"delivery"`
				}
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if got.Status != "accepted" || got.Event != "pull_request" || got.Delivery != "abc-123" {
					t.Fatalf("body=%+v", got)
				}
			},
		},
		{
			name:     "tampered body rejected",
			secret:   secret,
			body:     []byte(`{"action":"closed"}`),
			headers:  map[string]string{"X-Hub-Signature-256": validSig},
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "dev mode accepts unsigned",
			body:     signedBody,
			wantCode: http.StatusAccepted,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := buildMux(healthz.NewRegistry(), silentLogger(), tc.secret, nil)
			req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(tc.body))
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.checkOK != nil {
				tc.checkOK(t, rec.Body.Bytes())
			}
		})
	}
}

func TestHeaderTenantExtractor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
		wantErr error
		wantT   tenancy.Context
	}{
		{
			name:    "valid tenant and workspace",
			headers: map[string]string{"X-Optiqor-Tenant": "tenant-1", "X-Optiqor-Workspace": "ws-1"},
			wantT:   tenancy.Context{TenantID: "tenant-1", WorkspaceID: "ws-1"},
		},
		{
			name:    "no header returns ErrNoTenant",
			wantErr: tenancy.ErrNoTenant,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			got, err := HeaderTenantExtractor(req)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.TenantID != tc.wantT.TenantID || got.WorkspaceID != tc.wantT.WorkspaceID {
				t.Errorf("extractor lost values: %+v", got)
			}
		})
	}
}

func TestRequireTenant(t *testing.T) {
	for _, tc := range []struct {
		name     string
		headers  map[string]string
		wantCode int
		wantInne bool
	}{
		{
			name:     "allows when tenant header set",
			headers:  map[string]string{"X-Optiqor-Tenant": "t1"},
			wantCode: http.StatusOK,
			wantInne: true,
		},
		{
			name:     "rejects without tenant",
			wantCode: http.StatusUnauthorized,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if called != tc.wantInne {
				t.Fatalf("inner called = %v, want %v", called, tc.wantInne)
			}
		})
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

func TestPanicRecovery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		inner    http.Handler
		wantCode int
		wantBody string
	}{
		{
			name: "captures panic and returns 500",
			inner: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic("kaboom")
			}),
			wantCode: http.StatusInternalServerError,
			wantBody: "internal server error",
		},
		{
			name: "passes through when no panic",
			inner: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
				_, _ = w.Write([]byte("hi"))
			}),
			wantCode: http.StatusTeapot,
			wantBody: "hi",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := withPanicRecovery(silentLogger(), tc.inner)
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", http.NoBody))
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body = %q, want substring %q", rec.Body.String(), tc.wantBody)
			}
		})
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

func TestRecordingWriter(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, rw *recordingWriter)
	}{
		{
			name: "implicit write keeps default 200",
			run: func(t *testing.T, rw *recordingWriter) {
				t.Helper()
				if _, err := rw.Write([]byte("hello")); err != nil {
					t.Fatal(err)
				}
				if rw.status != 200 {
					t.Errorf("status = %d", rw.status)
				}
				if rw.bytes != 5 {
					t.Errorf("bytes = %d", rw.bytes)
				}
			},
		},
		{
			name: "second WriteHeader is ignored",
			run: func(t *testing.T, rw *recordingWriter) {
				t.Helper()
				rw.WriteHeader(http.StatusBadRequest)
				rw.WriteHeader(http.StatusInternalServerError)
				if rw.status != http.StatusBadRequest {
					t.Errorf("status = %d, want 400", rw.status)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rw := &recordingWriter{ResponseWriter: rec, status: 200}
			tc.run(t, rw)
		})
	}
}

func TestGitHubOAuthCallback(t *testing.T) {
	for _, tc := range []struct {
		name     string
		url      string
		wantCode int
		wantBody string
	}{
		{
			name:     "missing code is 400",
			url:      "/oauth/github/callback",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "code accepted",
			url:      "/oauth/github/callback?state=abc&code=xyz",
			wantCode: http.StatusOK,
			wantBody: `"phase":"1"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, http.NoBody))
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body missing %q: %s", tc.wantBody, rec.Body.String())
			}
		})
	}
}

func TestPProf_GatedByAdminToken(t *testing.T) {
	for _, tc := range []struct {
		name     string
		token    string
		wantCode int
	}{
		{"missing token", "", http.StatusUnauthorized},
		{"wrong token", "wrong", http.StatusUnauthorized},
		{"correct token", "secret-token", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mountPProf(mux, "secret-token")
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", http.NoBody)
			if tc.token != "" {
				req.Header.Set("X-Admin-Token", tc.token)
			}
			mux.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
		})
	}
}

func TestSubtleConstantTimeEq(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "abc", "abc", 1},
		{"different byte", "abc", "abd", 0},
		{"different length", "abc", "ab", 0},
		{"both empty", "", "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := subtleConstantTimeEq(tc.a, tc.b); got != tc.want {
				t.Errorf("eq(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
