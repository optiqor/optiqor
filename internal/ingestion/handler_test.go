package ingestion

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/optiqor/backend/internal/tenancy"
)

func TestHandler_RejectsNonPost(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/v1/ingest", http.NoBody)
	w := httptest.NewRecorder()
	h.Ingest(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d", w.Code)
	}
}

func TestHandler_EmptyPayload_400(t *testing.T) {
	h := &Handler{}
	body, _ := json.Marshal(IngestRequest{Tenant: "t1"})
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.Ingest(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d body = %s", w.Code, w.Body.String())
	}
}

func TestHandler_MissingTenant_400(t *testing.T) {
	h := &Handler{}
	body, _ := json.Marshal(IngestRequest{PrometheusJSON: []byte(promMatrixOK)})
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.Ingest(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d", w.Code)
	}
}

func TestHandler_PrometheusOnly_RoutesToSink(t *testing.T) {
	var mu sync.Mutex
	var got []PromSeries
	h := &Handler{
		PromSink: func(t tenancy.Context, s []PromSeries) error {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, s...)
			return nil
		},
	}
	body, _ := json.Marshal(IngestRequest{
		Tenant:         "t1",
		ClusterID:      "c1",
		PrometheusJSON: []byte(promMatrixOK),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.Ingest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", w.Code, w.Body.String())
	}
	if len(got) != 1 {
		t.Errorf("got %d series", len(got))
	}
	var resp IngestResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.PromSeries != 1 {
		t.Errorf("resp.PromSeries = %d, want 1", resp.PromSeries)
	}
}

func TestHandler_CURRowsOnly_RoutesToSink(t *testing.T) {
	var got []CURRow
	h := &Handler{
		CURSink: func(_ tenancy.Context, rows []CURRow) error {
			got = append(got, rows...)
			return nil
		},
	}
	body, _ := json.Marshal(IngestRequest{
		Tenant:     "t1",
		CURRowsCSV: []byte(curOK),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.Ingest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", w.Code, w.Body.String())
	}
	if len(got) != 2 {
		t.Errorf("got %d rows", len(got))
	}
}

func TestHandler_MalformedPromBytes_400(t *testing.T) {
	h := &Handler{}
	body, _ := json.Marshal(IngestRequest{
		Tenant:         "t1",
		PrometheusJSON: []byte(`{"status":"error","data":{"resultType":"matrix","result":[]}}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	h.Ingest(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d body = %s", w.Code, w.Body.String())
	}
}
