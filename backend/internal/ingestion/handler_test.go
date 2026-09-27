package ingestion

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestHandler_Ingest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		method   string
		req      *IngestRequest // nil means send an empty JSON body
		omitCtx  bool           // skip the tenancy context (Phase-1: middleware would attach it)
		withProm bool           // wire a PromSink that records calls
		withCUR  bool           // wire a CURSink that records calls
		wantCode int
		check    func(t *testing.T, w *httptest.ResponseRecorder, prom [][]PromSeries, cur [][]CURRow)
	}{
		{
			name:     "rejects non-POST",
			method:   http.MethodGet,
			wantCode: http.StatusMethodNotAllowed,
		},
		{
			name:     "empty payload returns 400",
			method:   http.MethodPost,
			req:      &IngestRequest{Tenant: "t1"},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "missing tenant context returns 401",
			method:   http.MethodPost,
			req:      &IngestRequest{PrometheusJSON: []byte(promMatrixOK)},
			omitCtx:  true,
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "body tenant mismatches context returns 400",
			method:   http.MethodPost,
			req:      &IngestRequest{Tenant: "spoofed", PrometheusJSON: []byte(promMatrixOK)},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "prometheus-only routes to sink and echoes count",
			method:   http.MethodPost,
			req:      &IngestRequest{Tenant: "t1", ClusterID: "c1", PrometheusJSON: []byte(promMatrixOK)},
			withProm: true,
			wantCode: http.StatusOK,
			check: func(t *testing.T, w *httptest.ResponseRecorder, prom [][]PromSeries, _ [][]CURRow) {
				t.Helper()
				if len(prom) != 1 || len(prom[0]) != 1 {
					t.Errorf("prom sink invocations = %v", prom)
				}
				var resp IngestResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if resp.PromSeries != 1 {
					t.Errorf("resp.PromSeries = %d, want 1", resp.PromSeries)
				}
			},
		},
		{
			name:     "cur-only routes to sink and counts rows",
			method:   http.MethodPost,
			req:      &IngestRequest{Tenant: "t1", CURRowsCSV: []byte(curOK)},
			withCUR:  true,
			wantCode: http.StatusOK,
			check: func(t *testing.T, _ *httptest.ResponseRecorder, _ [][]PromSeries, cur [][]CURRow) {
				t.Helper()
				if len(cur) != 1 || len(cur[0]) != 2 {
					t.Errorf("cur sink invocations = %v", cur)
				}
			},
		},
		{
			name:     "malformed prometheus bytes returns 400",
			method:   http.MethodPost,
			req:      &IngestRequest{Tenant: "t1", PrometheusJSON: []byte(`{"status":"error","data":{"resultType":"matrix","result":[]}}`)},
			wantCode: http.StatusBadRequest,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mu      sync.Mutex
				promGot [][]PromSeries
				curGot  [][]CURRow
			)
			h := &Handler{}
			if tc.withProm {
				h.PromSink = func(_ tenancy.Context, s []PromSeries) error {
					mu.Lock()
					defer mu.Unlock()
					promGot = append(promGot, s)
					return nil
				}
			}
			if tc.withCUR {
				h.CURSink = func(_ tenancy.Context, rows []CURRow) error {
					mu.Lock()
					defer mu.Unlock()
					curGot = append(curGot, rows)
					return nil
				}
			}

			var body []byte
			if tc.req != nil {
				body, _ = json.Marshal(tc.req)
			}
			req := httptest.NewRequest(tc.method, "/v1/ingest", bytes.NewReader(body))
			if !tc.omitCtx {
				req = req.WithContext(tenancy.WithContext(req.Context(), tenancy.Context{TenantID: "t1"}))
			}
			w := httptest.NewRecorder()
			h.Ingest(w, req)
			if w.Code != tc.wantCode {
				t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
			}
			if tc.check != nil {
				mu.Lock()
				defer mu.Unlock()
				tc.check(t, w, promGot, curGot)
			}
		})
	}
}
