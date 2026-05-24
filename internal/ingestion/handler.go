package ingestion

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// IngestRequest carries Prometheus bytes, CUR bytes, or both. Requests
// with neither are rejected (see ErrEmptyIngest).
type IngestRequest struct {
	Tenant         string `json:"tenant"`
	ClusterID      string `json:"cluster_id"`
	PrometheusJSON []byte `json:"prometheus_json,omitempty"`
	CURRowsCSV     []byte `json:"cur_rows_csv,omitempty"`
}

// IngestResponse echoes parse counts so the agent can detect count
// mismatches and retry.
type IngestResponse struct {
	PromSeries int `json:"prom_series"`
	CURRows    int `json:"cur_rows"`
}

var ErrEmptyIngest = errors.New("ingestion: empty request, supply prometheus_json or cur_rows_csv")

type Handler struct {
	PromSink func(t tenancy.Context, series []PromSeries) error
	CURSink  func(t tenancy.Context, rows []CURRow) error
}

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

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/ingest", h.Ingest)
}
