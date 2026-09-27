package billing

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/platform/httperr"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// SpikeEnvelope is the union of fields AWS Cost Anomaly Detection,
// Azure Cost Management, and our internal poller emit so dispatch
// stays single-path.
type SpikeEnvelope struct {
	Tenant            string    `json:"tenant"`
	WorkloadID        string    `json:"workload_id"`
	ObservedDeltaUSD  float64   `json:"observed_delta_usd"`
	ObservedAtUTC     time.Time `json:"observed_at_utc"`
	LikelyPRCommitSHA string    `json:"likely_pr_commit_sha,omitempty"`
	LikelyPRURL       string    `json:"likely_pr_url,omitempty"`
}

// SpikeDispatcher is the seam cmd/api uses to wire the worker's
// CostSpike workflow without the handler depending on the worker.
type SpikeDispatcher interface {
	DispatchSpike(t tenancy.Context, ev SpikeEnvelope) error
}

// SpikeHandler serves POST /v1/cost-spikes. Phase-5 binds the inbound
// path to per-source signature verification (AWS SNS message signing,
// Azure Event Grid keys); until then env.Tenant is body-trusted and
// the route is firewalled at the LB to known anomaly-detector source
// IPs. Don't expose this route on a public LB without that gate.
type SpikeHandler struct {
	Dispatcher SpikeDispatcher
}

func (h *SpikeHandler) Receive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httperr.MethodNotAllowed(w, r, "POST")
		return
	}
	if h.Dispatcher == nil {
		httperr.Internal(w, r, "cost-spike dispatcher not configured")
		return
	}
	body := http.MaxBytesReader(w, r.Body, config.BillingSpikeWebhookMaxBytes)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var env SpikeEnvelope
	if err := dec.Decode(&env); err != nil {
		if httperr.IsBodyTooLarge(err) {
			httperr.BodyTooLarge(w, r, config.BillingSpikeWebhookMaxBytes)
			return
		}
		httperr.InvalidJSON(w, r, err)
		return
	}
	if env.Tenant == "" {
		httperr.MissingField(w, r, "tenant")
		return
	}
	if env.WorkloadID == "" {
		httperr.MissingField(w, r, "workload_id")
		return
	}

	if err := h.Dispatcher.DispatchSpike(tenancy.Context{TenantID: env.Tenant}, env); err != nil {
		httperr.Upstream(w, r, "could not dispatch cost-spike workflow")
		return
	}
	w.Header().Set("Retry-After", "5")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":              "accepted",
		"workflow":            "cost_spike",
		"retry_after_seconds": 5,
	})
}

func (h *SpikeHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/cost-spikes", h.Receive)
}
