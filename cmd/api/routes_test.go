package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/optiqor/backend/internal/platform/healthz"
)

// fullMux mirrors what cmd/api would assemble in production: the
// platform routes plus every domain handler. Used to verify the
// end-to-end wiring is correct without spinning a real socket.
func fullMux() *http.ServeMux {
	mux := buildMux(healthz.NewRegistry(), silentLogger(), nil, nil)
	mountDomainRoutes(mux, buildDomainDeps())
	mux.HandleFunc("GET /v1/meta", metaHandler)
	return mux
}

func TestRoutes_Analyze_Reachable(t *testing.T) {
	mux := fullMux()
	body := `api:
  resources:
    requests: {cpu: 500m, memory: 256Mi}`
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "accuracy_disclosure") {
		t.Errorf("missing disclosure in body:\n%s", w.Body.String())
	}
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
	want := []string{"/v1/analyze", "/r/{hash}", "/v1/receipts/{id}", "/v1/apply-fixes", "/v1/ingest", "/v1/cost-spikes"}
	for _, w := range want {
		found := false
		for _, e := range got.Endpoints {
			if e.Path == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("meta missing endpoint %q", w)
		}
	}
}

func TestRoutes_ApplyFixes_RequiresTenant(t *testing.T) {
	mux := fullMux()
	body := `{"chart":"x","chart_yaml":"a: 1","model":"claude-sonnet","finding":{"DetectorID":"x"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apply-fixes", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusForbidden {
		t.Errorf("code = %d, want 401/403", w.Code)
	}
}

func TestRoutes_ApplyFixes_AcceptsTenantHeader(t *testing.T) {
	mux := fullMux()
	body := `{"chart":"x","chart_yaml":"a: 1","model":"claude-sonnet","finding":{"DetectorID":"x"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apply-fixes", strings.NewReader(body))
	req.Header.Set("X-Optiqor-Tenant", "tenant-abc")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("code = %d body = %s", w.Code, w.Body.String())
	}
}

func TestRoutes_Receipts_404OnMissing(t *testing.T) {
	mux := fullMux()
	req := httptest.NewRequest(http.MethodGet, "/v1/receipts/missing-id", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d", w.Code)
	}
}

func TestRoutes_Ingest_400OnEmpty(t *testing.T) {
	mux := fullMux()
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", strings.NewReader(`{"tenant":"t1"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d body = %s", w.Code, w.Body.String())
	}
}

func TestRoutes_CostSpike_202(t *testing.T) {
	mux := fullMux()
	req := httptest.NewRequest(http.MethodPost, "/v1/cost-spikes",
		strings.NewReader(`{"tenant":"t1","workload_id":"wl-1","observed_delta_usd":120}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Errorf("code = %d body = %s", w.Code, w.Body.String())
	}
}
