package ingestion

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/platform/httperr"
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
		httperr.MethodNotAllowed(w, r, "POST")
		return
	}
	t, err := tenancy.FromContext(r.Context())
	if err != nil {
		httperr.Unauthorized(w, r, "tenant context required — agents authenticate via mTLS SPIFFE id")
		return
	}

	body := http.MaxBytesReader(w, r.Body, config.IngestMaxBytes)
	defer func() { _ = r.Body.Close() }()

	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req IngestRequest
	if err := dec.Decode(&req); err != nil {
		if httperr.IsBodyTooLarge(err) {
			httperr.BodyTooLarge(w, r, config.IngestMaxBytes)
			return
		}
		httperr.InvalidJSON(w, r, err)
		return
	}
	if req.Tenant != "" && req.Tenant != t.TenantID {
		httperr.WriteWithDetails(w, r, http.StatusBadRequest, "TENANT_MISMATCH",
			"tenant in body does not match the authenticated tenant context",
			map[string]any{"body_tenant": req.Tenant, "context_tenant": t.TenantID})
		return
	}
	if req.ClusterID != "" {
		t.ClusterID = req.ClusterID
	}
	if len(req.PrometheusJSON) == 0 && len(req.CURRowsCSV) == 0 {
		httperr.BadRequest(w, r, ErrEmptyIngest.Error())
		return
	}

	resp := IngestResponse{}

	if len(req.PrometheusJSON) > 0 {
		series, err := ParsePrometheusMatrix(bytesReader(req.PrometheusJSON))
		if err != nil {
			httperr.WriteWithDetails(w, r, http.StatusBadRequest, "PROMETHEUS_PARSE_ERROR",
				"could not parse prometheus_json: "+err.Error(),
				map[string]any{"hint": "expect a Prometheus query_range matrix response body"})
			return
		}
		if h.PromSink != nil {
			if err := h.PromSink(t, series); err != nil {
				httperr.Internal(w, r, "could not persist Prometheus series")
				return
			}
		}
		resp.PromSeries = len(series)
	}
	if len(req.CURRowsCSV) > 0 {
		rows, err := ParseCURRows(bytesReader(req.CURRowsCSV))
		if err != nil {
			httperr.WriteWithDetails(w, r, http.StatusBadRequest, "CUR_PARSE_ERROR",
				"could not parse cur_rows_csv: "+err.Error(),
				map[string]any{"hint": "expect AWS CUR rows in canonical CSV order"})
			return
		}
		if h.CURSink != nil {
			if err := h.CURSink(t, rows); err != nil {
				httperr.Internal(w, r, "could not persist CUR rows")
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
