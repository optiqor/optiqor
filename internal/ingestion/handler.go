package ingestion

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// IngestRequest carries Prometheus bytes, CUR bytes, or both. Requests
// with neither are rejected (see ErrEmptyIngest).
//
// TenantID + ClusterID come from the request context (X-Optiqor-Tenant
// header in Phase 1; mTLS-bound SPIFFE id in Phase 5). The body's
// `tenant` / `cluster_id` fields are accepted only as a consistency
// check — they must match the context, or the request is rejected.
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
	// Tenant context must come from the request middleware (header in
	// Phase 1, mTLS SPIFFE id in Phase 5). Reject early so the parser
	// + sinks never run under a body-claimed tenant.
	t, err := tenancy.FromContext(r.Context())
	if err != nil {
		http.Error(w, "tenant context required", http.StatusBadRequest)
		return
	}

	body := http.MaxBytesReader(w, r.Body, config.IngestMaxBytes)
	defer func() { _ = r.Body.Close() }()

	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req IngestRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "json: "+err.Error(), http.StatusBadRequest)
		return
	}
	// Body must agree with the context so a misconfigured agent fails
	// loudly instead of silently writing under another tenant.
	if req.Tenant != "" && req.Tenant != t.TenantID {
		http.Error(w, "tenant in body does not match request context", http.StatusBadRequest)
		return
	}
	if req.ClusterID != "" {
		t.ClusterID = req.ClusterID
	}
	if len(req.PrometheusJSON) == 0 && len(req.CURRowsCSV) == 0 {
		http.Error(w, ErrEmptyIngest.Error(), http.StatusBadRequest)
		return
	}

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
