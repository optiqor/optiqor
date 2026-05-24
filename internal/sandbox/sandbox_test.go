package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/cost"
	"github.com/optiqor/optiqor/internal/platform/config"
)

const exampleChart = `api:
  replicas: 3
  resources:
    requests: {cpu: 500m, memory: 256Mi}
    limits:   {cpu: 1, memory: 512Mi}
  image: nginx:1.25
worker:
  resources:
    requests: {cpu: 200m, memory: 128Mi}
`

func newHandler() *Handler {
	return &Handler{
		Store:  NewInMemoryStore(),
		Pricer: cost.NewStaticPricer(),
		Region: "us-east-1",
		Now:    func() time.Time { return time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC) },
	}
}

func TestAnalyze(t *testing.T) {
	for _, tc := range []struct {
		name       string
		handler    func() *Handler
		req        func() *http.Request
		wantStatus int
		check      func(t *testing.T, h *Handler, rec *httptest.ResponseRecorder)
	}{
		{
			name:    "rejects non-post",
			handler: newHandler,
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "/v1/analyze", http.NoBody)
			},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:    "happy path",
			handler: newHandler,
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(exampleChart))
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, _ *Handler, rec *httptest.ResponseRecorder) {
				t.Helper()
				if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
					t.Errorf("content-type = %q", ct)
				}
				var resp AnalyzeResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("unmarshal: %v\nbody:%s", err, rec.Body.String())
				}
				if resp.AccuracyDisclosure != AccuracyDisclosure {
					t.Errorf("disclosure mismatch: %q", resp.AccuracyDisclosure)
				}
				if resp.Workloads != 2 {
					t.Errorf("workloads = %d, want 2", resp.Workloads)
				}
				if resp.ShareHash == "" {
					t.Error("share_hash empty")
				}
				if !strings.Contains(resp.ShareURL, "/r/"+resp.ShareHash) {
					t.Errorf("share_url = %q, missing /r/<hash> suffix", resp.ShareURL)
				}
				if len(resp.CostFindings)+len(resp.SecurityFindingsBonus) != len(resp.Findings) {
					t.Errorf("split mismatch: cost=%d security=%d findings=%d",
						len(resp.CostFindings), len(resp.SecurityFindingsBonus), len(resp.Findings))
				}
				if resp.AnnualSavingsUSD != resp.MonthlySavingsUSD*12 {
					t.Errorf("annual != monthly*12: %v vs %v", resp.AnnualSavingsUSD, resp.MonthlySavingsUSD)
				}
			},
		},
		{
			name:    "share url derived from request in dev",
			handler: newHandler,
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "http://localhost:3000/v1/analyze", strings.NewReader(exampleChart))
				r.Host = "localhost:3000"
				return r
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, _ *Handler, rec *httptest.ResponseRecorder) {
				t.Helper()
				var resp AnalyzeResponse
				_ = json.Unmarshal(rec.Body.Bytes(), &resp)
				if !strings.HasPrefix(resp.ShareURL, "http://localhost:3000/r/") {
					t.Errorf("share_url should derive from request: got %q", resp.ShareURL)
				}
			},
		},
		{
			name: "share url honours public base url",
			handler: func() *Handler {
				h := newHandler()
				h.PublicBaseURL = "https://optiqor.dev"
				return h
			},
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "http://localhost:3000/v1/analyze", strings.NewReader(exampleChart))
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, _ *Handler, rec *httptest.ResponseRecorder) {
				t.Helper()
				var resp AnalyzeResponse
				_ = json.Unmarshal(rec.Body.Bytes(), &resp)
				if !strings.HasPrefix(resp.ShareURL, "https://optiqor.dev/r/") {
					t.Errorf("share_url should honour configured PublicBaseURL: got %q", resp.ShareURL)
				}
			},
		},
		{
			name:    "share url honours x-forwarded-proto",
			handler: newHandler,
			req: func() *http.Request {
				r := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(exampleChart))
				r.Host = "optiqor.dev"
				r.Header.Set("X-Forwarded-Proto", "https")
				return r
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, _ *Handler, rec *httptest.ResponseRecorder) {
				t.Helper()
				var resp AnalyzeResponse
				_ = json.Unmarshal(rec.Body.Bytes(), &resp)
				if !strings.HasPrefix(resp.ShareURL, "https://optiqor.dev/r/") {
					t.Errorf("X-Forwarded-Proto should drive scheme: got %q", resp.ShareURL)
				}
			},
		},
		{
			name:    "bad yaml 400",
			handler: newHandler,
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(":\n  - not: [valid"))
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "oversized body 413",
			handler: newHandler,
			req: func() *http.Request {
				big := strings.Repeat("a", int(config.SandboxAnalyzeMaxBytes)+1)
				return httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(big))
			},
			wantStatus: http.StatusRequestEntityTooLarge,
		},
		{
			name:    "disclosure always present",
			handler: newHandler,
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(exampleChart))
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, _ *Handler, rec *httptest.ResponseRecorder) {
				t.Helper()
				body, _ := io.ReadAll(rec.Body)
				if !strings.Contains(string(body), "±40%") {
					t.Errorf("response missing accuracy disclosure:\n%s", body)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.handler()
			rec := httptest.NewRecorder()
			h.Analyze(rec, tc.req())
			if rec.Code != tc.wantStatus {
				t.Fatalf("status: got %d want %d body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.check != nil {
				tc.check(t, h, rec)
			}
		})
	}
}

func TestShare_RoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name        string
		acceptHdr   string
		query       string
		wantContent string // substring expected in body
		wantCT      string // prefix expected on Content-Type
	}{
		{
			name:        "default html",
			wantContent: "<!doctype html>",
			wantCT:      "text/html",
		},
		{
			name:        "accept json",
			acceptHdr:   "application/json",
			wantContent: "accuracy_disclosure",
		},
		{
			name:        "format query",
			query:       "?format=json",
			wantContent: "accuracy_disclosure",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHandler()
			req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(exampleChart))
			rec := httptest.NewRecorder()
			h.Analyze(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("analyze failed: %s", rec.Body.String())
			}
			var resp AnalyzeResponse
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)

			mux := http.NewServeMux()
			h.Mount(mux)
			getReq := httptest.NewRequest(http.MethodGet, "/r/"+resp.ShareHash+tc.query, http.NoBody)
			if tc.acceptHdr != "" {
				getReq.Header.Set("Accept", tc.acceptHdr)
			}
			getRec := httptest.NewRecorder()
			mux.ServeHTTP(getRec, getReq)
			if getRec.Code != http.StatusOK {
				t.Fatalf("share GET code = %d body=%s", getRec.Code, getRec.Body.String())
			}
			if tc.wantCT != "" {
				if ct := getRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tc.wantCT) {
					t.Errorf("content-type = %q, want prefix %q", ct, tc.wantCT)
				}
			}
			body := getRec.Body.String()
			if !strings.Contains(body, tc.wantContent) {
				t.Errorf("body missing %q:\n%s", tc.wantContent, body)
			}
			if tc.wantCT == "text/html" && !strings.Contains(body, "Sandbox accuracy: ±40%") {
				t.Errorf("HTML share missing accuracy disclosure")
			}
		})
	}
}

func TestShare_404OnMissing(t *testing.T) {
	h := newHandler()
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/r/deadbeef", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d", w.Code)
	}
}

func TestShare_RespectsExpiry(t *testing.T) {
	store := NewInMemoryStore()
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now.Add(31 * 24 * time.Hour) }
	_ = store.Put(context.Background(), SharedAnalysis{
		Hash:      "x",
		Body:      []byte("{}"),
		MediaType: "application/json",
		ExpiresAt: now.Add(1 * time.Hour),
	})
	if _, err := store.Get(context.Background(), "x"); err == nil {
		t.Error("expected ErrNotFound for expired entry")
	}
}

func TestHashBytes_StableAcrossCalls(t *testing.T) {
	a := hashBytes([]byte("hello"))
	b := hashBytes([]byte("hello"))
	if a != b {
		t.Errorf("hash non-deterministic: %s vs %s", a, b)
	}
	// 12 bytes hex-encoded.
	if len(a) != 24 {
		t.Errorf("hash length = %d, want 24", len(a))
	}
}
