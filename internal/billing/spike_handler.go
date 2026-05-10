package billing

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/optiqor/backend/internal/tenancy"
)

// SpikeEnvelope is the wire shape of a cost-spike event delivered by
// AWS Cost Anomaly Detection (SNS payload), Azure Cost Management, or
// our own internal poller. The fields are deliberately the union of
// what those upstreams emit so the dispatch logic stays single-path.
type SpikeEnvelope struct {
	Tenant            string    `json:"tenant"`
	WorkloadID        string    `json:"workload_id"`
	ObservedDeltaUSD  float64   `json:"observed_delta_usd"`
	ObservedAtUTC     time.Time `json:"observed_at_utc"`
	LikelyPRCommitSHA string    `json:"likely_pr_commit_sha,omitempty"`
	LikelyPRURL       string    `json:"likely_pr_url,omitempty"`
}

// SpikeDispatcher is the seam between the webhook and the worker
// dispatcher. cmd/api wires the worker's CostSpike workflow behind
// this interface so the handler itself stays test-friendly.
type SpikeDispatcher interface {
	DispatchSpike(t tenancy.Context, ev SpikeEnvelope) error
}

// SpikeHandler serves POST /v1/cost-spikes. We never trust the
// upstream signature alone — every event is logged and dispatched,
// and the worker decides whether to act on it.
type SpikeHandler struct {
	Dispatcher SpikeDispatcher
}

const maxSpikeBytes = 64 << 10 // 64 KiB — AWS Cost Anomaly payloads are tiny

// Receive validates the payload and dispatches the spike event.
func (h *SpikeHandler) Receive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Dispatcher == nil {
		http.Error(w, "dispatcher not configured", http.StatusInternalServerError)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSpikeBytes))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	defer r.Body.Close()

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

// Mount registers the route on a mux.
func (h *SpikeHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/cost-spikes", h.Receive)
}
