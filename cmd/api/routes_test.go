package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/platform/healthz"
)

// fullMux mirrors the production assembly: platform routes + every
// domain handler. Lets the wiring tests skip the real socket.
func fullMux() *http.ServeMux {
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, nil)
	mountDomainRoutes(mux, buildDomainDeps())
	mux.HandleFunc("GET /v1/meta", metaHandler)
	return mux
}

func TestRoutes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		method    string
		path      string
		body      io.Reader
		headers   map[string]string
		wantCodes []int
		bodyHas   []string
	}{
		{
			name:   "analyze reachable",
			method: http.MethodPost,
			path:   "/v1/analyze",
			body: strings.NewReader(`api:
  resources:
    requests: {cpu: 500m, memory: 256Mi}`),
			wantCodes: []int{http.StatusOK},
			bodyHas:   []string{"accuracy_disclosure"},
		},
		{
			name:      "apply-fixes requires tenant",
			method:    http.MethodPost,
			path:      "/v1/apply-fixes",
			body:      strings.NewReader(`{"chart":"x","chart_yaml":"a: 1","model":"claude-sonnet","finding":{"DetectorID":"x"}}`),
			wantCodes: []int{http.StatusUnauthorized, http.StatusForbidden},
		},
		{
			name:      "apply-fixes accepts tenant header",
			method:    http.MethodPost,
			path:      "/v1/apply-fixes",
			body:      strings.NewReader(`{"chart":"x","chart_yaml":"a: 1","model":"claude-sonnet","finding":{"DetectorID":"x"}}`),
			headers:   map[string]string{"X-Optiqor-Tenant": "tenant-abc"},
			wantCodes: []int{http.StatusOK},
		},
		{
			name:      "receipts 404 on missing",
			method:    http.MethodGet,
			path:      "/v1/receipts/missing-id",
			wantCodes: []int{http.StatusNotFound},
		},
		{
			name:      "ingest 400 on empty",
			method:    http.MethodPost,
			path:      "/v1/ingest",
			body:      strings.NewReader(`{"tenant":"tenant-abc"}`),
			headers:   map[string]string{"X-Optiqor-Tenant": "tenant-abc"},
			wantCodes: []int{http.StatusBadRequest},
		},
		{
			name:      "ingest 401 without tenant header",
			method:    http.MethodPost,
			path:      "/v1/ingest",
			body:      strings.NewReader(`{"tenant":"tenant-abc"}`),
			wantCodes: []int{http.StatusUnauthorized},
		},
		{
			name:      "cost-spike accepted",
			method:    http.MethodPost,
			path:      "/v1/cost-spikes",
			body:      strings.NewReader(`{"tenant":"t1","workload_id":"wl-1","observed_delta_usd":120}`),
			wantCodes: []int{http.StatusAccepted},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := fullMux()
			body := tc.body
			if body == nil {
				body = http.NoBody
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if !codeIn(w.Code, tc.wantCodes) {
				t.Fatalf("code = %d, want one of %v; body=%s", w.Code, tc.wantCodes, w.Body.String())
			}
			for _, want := range tc.bodyHas {
				if !strings.Contains(w.Body.String(), want) {
					t.Errorf("body missing %q:\n%s", want, w.Body.String())
				}
			}
		})
	}
}

func codeIn(got int, want []int) bool {
	for _, c := range want {
		if c == got {
			return true
		}
	}
	return false
}

func TestRoutes_Meta_ListsKnownEndpoints(t *testing.T) {
	mux := fullMux()
	req := httptest.NewRequest(http.MethodGet, "/v1/meta", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	var got struct {
		Endpoints []struct {
			Path string `json:"path"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/v1/analyze", "/r/{hash}", "/v1/receipts/{id}", "/v1/apply-fixes", "/v1/ingest", "/v1/cost-spikes"} {
		found := false
		for _, e := range got.Endpoints {
			if e.Path == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("meta missing endpoint %q", want)
		}
	}
}
