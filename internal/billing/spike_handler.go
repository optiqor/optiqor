package billing

import (
	"encoding/json"
	"io"
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

// SpikeHandler serves POST /v1/cost-spikes. The handler always
// dispatches; the worker decides whether to act on it.
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
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, config.BillingSpikeWebhookMaxBytes))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer func() { _ = r.Body.Close() }()

	var env SpikeEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
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
