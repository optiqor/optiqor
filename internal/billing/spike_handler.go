package billing

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/optiqor/optiqor/internal/platform/config"
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Dispatcher == nil {
		http.Error(w, "dispatcher not configured", http.StatusInternalServerError)
		return
	}
	body := http.MaxBytesReader(w, r.Body, config.BillingSpikeWebhookMaxBytes)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var env SpikeEnvelope
	if err := dec.Decode(&env); err != nil {
		http.Error(w, "json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if env.Tenant == "" || env.WorkloadID == "" {
		http.Error(w, "tenant and workload_id required", http.StatusBadRequest)
		return
	}

	if err := h.Dispatcher.DispatchSpike(tenancy.Context{TenantID: env.Tenant}, env); err != nil {
		http.Error(w, "dispatch: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *SpikeHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/cost-spikes", h.Receive)
}
