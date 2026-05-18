package ingestion

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// IngestRequest is the wire shape the in-cluster agent ships. One
// request carries either Prometheus matrix bytes, CUR-row bytes, or
// both — but never neither.
type IngestRequest struct {
	Tenant         string `json:"tenant"`
	ClusterID      string `json:"cluster_id"`
	PrometheusJSON []byte `json:"prometheus_json,omitempty"`
	CURRowsCSV     []byte `json:"cur_rows_csv,omitempty"`
}

// IngestResponse acks the parse. The series + row counts are echoed
// so the agent can decide whether to retry on a count mismatch.
type IngestResponse struct {
	PromSeries int `json:"prom_series"`
	CURRows    int `json:"cur_rows"`
}

// ErrEmptyIngest is returned when a request carries neither
// Prometheus nor CUR bytes.
var ErrEmptyIngest = errors.New("ingestion: empty request — supply prometheus_json or cur_rows_csv")

// Handler serves POST /v1/ingest.
type Handler struct {
	// PromSink and CURSink are where parsed records land. Phase 1
	// implementations just write to Postgres; tests use in-memory
	// sinks to assert on the parsed shape.
	PromSink func(t tenancy.Context, series []PromSeries) error
	CURSink  func(t tenancy.Context, rows []CURRow) error
}

// Ingest parses incoming bytes and routes them to the configured
// sinks. Returns counts so the caller can verify lossless ingestion.
//
//	400 — malformed JSON / CSV
//	413 — body exceeds config.IngestMaxBytes
func (h *Handler) Ingest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, config.IngestMaxBytes))
	if err != nil {
		http.Error(w, "read: "+err.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	defer func() { _ = r.Body.Close() }()

	var req IngestRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Tenant == "" {
		http.Error(w, "tenant required", http.StatusBadRequest)
		return
	}
	if len(req.PrometheusJSON) == 0 && len(req.CURRowsCSV) == 0 {
		http.Error(w, ErrEmptyIngest.Error(), http.StatusBadRequest)
		return
	}

	t := tenancy.Context{TenantID: req.Tenant, ClusterID: req.ClusterID}
	resp := IngestResponse{}

	if len(req.PrometheusJSON) > 0 {
		series, err := ParsePrometheusMatrix(bytesReader(req.PrometheusJSON))
		if err != nil {
			http.Error(w, "prometheus: "+err.Error(), http.StatusBadRequest)
			return
		}
		if h.PromSink != nil {
			if err := h.PromSink(t, series); err != nil {
				http.Error(w, "prom sink: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		resp.PromSeries = len(series)
	}
	if len(req.CURRowsCSV) > 0 {
		rows, err := ParseCURRows(bytesReader(req.CURRowsCSV))
		if err != nil {
			http.Error(w, "cur: "+err.Error(), http.StatusBadRequest)
			return
		}
		if h.CURSink != nil {
			if err := h.CURSink(t, rows); err != nil {
				http.Error(w, "cur sink: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		resp.CURRows = len(rows)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// Mount registers the route on a mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/ingest", h.Ingest)
}
