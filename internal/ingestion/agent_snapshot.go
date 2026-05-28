package ingestion

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/optiqor/optiqor/internal/agent/ident"
	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/platform/httperr"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// AgentSnapshot is the rich payload the in-cluster agent POSTs every
// 60s. K8s inventory + Prom samples + service graph + provisioner
// detection arrive in one round-trip so the api commits one
// transaction per snapshot. mTLS (SPIFFE URI SAN) authenticates the
// tenant; the JWT in X-Optiqor-Agent-Token proves recency.
type AgentSnapshot struct {
	BatchID          string           `json:"batch_id"`
	CapturedAt       time.Time        `json:"captured_at"`
	ClusterID        string           `json:"cluster_id"`
	AgentVersion     string           `json:"agent_version"`
	Workloads        []WorkloadSnap   `json:"workloads,omitempty"`
	Events           []EventSnap      `json:"events,omitempty"`
	HPAs             []HPASnap        `json:"hpas,omitempty"`
	Policies         []PolicySnap     `json:"policies,omitempty"`
	NodePools        []NodePoolSnap   `json:"node_pools,omitempty"`
	ProvisionerClass string           `json:"provisioner_class,omitempty"`
	Health           *AgentHealthSnap `json:"health,omitempty"`
}

type WorkloadSnap struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
}

type EventSnap struct {
	Namespace string    `json:"namespace"`
	PodName   string    `json:"pod_name"`
	Reason    string    `json:"reason"`
	Type      string    `json:"type"`
	Count     int       `json:"count"`
	LastSeen  time.Time `json:"last_seen"`
	Message   string    `json:"message"`
}

type HPASnap struct {
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	MinReplicas     int    `json:"min_replicas"`
	MaxReplicas     int    `json:"max_replicas"`
	CurrentReplicas int    `json:"current_replicas"`
}

type PolicySnap struct {
	Namespace   string `json:"namespace"`
	HasPDB      bool   `json:"has_pdb"`
	HasQuota    bool   `json:"has_quota"`
	HasLimitRng bool   `json:"has_limit_range"`
}

type NodePoolSnap struct {
	Name              string              `json:"name"`
	NodeCountCurrent  int                 `json:"node_count_current"`
	ConsolidationMode string              `json:"consolidation_mode,omitempty"`
	Requirements      map[string][]string `json:"requirements,omitempty"`
}

type AgentHealthSnap struct {
	Version              string `json:"version"`
	DataFreshnessSeconds int    `json:"data_freshness_seconds"`
}

type AgentSnapshotResponse struct {
	Accepted     bool   `json:"accepted"`
	BatchID      string `json:"batch_id"`
	WorkloadsObs int    `json:"workloads_observed"`
	EventsObs    int    `json:"events_observed"`
}

// AgentSnapshotSink persists a verified snapshot. nil-safe: in dev
// the api binary boots without Postgres and the handler short-circuits
// to 202 Accepted.
type AgentSnapshotSink interface {
	Persist(ctx tenancy.Context, snap AgentSnapshot) error
}

// AgentSnapshotHandler is the POST /v1/agent/snapshot endpoint. mTLS
// middleware already extracted the tenant; this handler verifies the
// JWT body audience + freshness and persists the snapshot.
type AgentSnapshotHandler struct {
	Verifier *ident.Verifier
	Sink     AgentSnapshotSink
}

// AgentSnapshotAudience is the constant the agent's Issuer signs into
// every token; the verifier rejects anything else. Treat as a public
// constant — changing it is a wire break.
const AgentSnapshotAudience = "agent-snapshot"

// TokenHeader names the HTTP header the agent attaches the short-
// lived JWT to. Separate from Authorization: Bearer to avoid
// confusion with the dashboard's session token.
const TokenHeader = "X-Optiqor-Agent-Token" //nolint:gosec // G101: header name, not a credential

func (h *AgentSnapshotHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httperr.MethodNotAllowed(w, r, "POST")
		return
	}
	t, err := tenancy.FromContext(r.Context())
	if err != nil {
		httperr.Unauthorized(w, r, "tenant context required — agent must present mTLS client cert")
		return
	}

	wire := r.Header.Get(TokenHeader)
	if wire == "" {
		httperr.Unauthorized(w, r, "missing "+TokenHeader+" header")
		return
	}
	if h.Verifier == nil {
		httperr.Internal(w, r, "agent token verifier not configured")
		return
	}
	tok, err := h.Verifier.Verify(wire, AgentSnapshotAudience)
	if err != nil {
		httperr.Unauthorized(w, r, "agent token: "+err.Error())
		return
	}
	if tok.TenantID != t.TenantID {
		httperr.WriteWithDetails(w, r, http.StatusUnauthorized, httperr.CodeUnauthorized,
			"agent token tenant does not match mTLS tenant",
			map[string]any{"cert_tenant": t.TenantID, "token_tenant": tok.TenantID})
		return
	}

	body := http.MaxBytesReader(w, r.Body, config.IngestMaxBytes)
	defer func() { _ = r.Body.Close() }()

	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var snap AgentSnapshot
	if err := dec.Decode(&snap); err != nil {
		if httperr.IsBodyTooLarge(err) {
			httperr.BodyTooLarge(w, r, config.IngestMaxBytes)
			return
		}
		httperr.InvalidJSON(w, r, err)
		return
	}
	if snap.ClusterID == "" || snap.BatchID == "" {
		httperr.MissingField(w, r, "cluster_id and batch_id")
		return
	}
	if tok.ClusterID != "" && tok.ClusterID != snap.ClusterID {
		httperr.WriteWithDetails(w, r, http.StatusBadRequest, "CLUSTER_MISMATCH",
			"snapshot.cluster_id does not match the token-bound cluster",
			map[string]any{"token_cluster": tok.ClusterID, "snap_cluster": snap.ClusterID})
		return
	}
	if h.Sink != nil {
		t.ClusterID = snap.ClusterID
		if err := h.Sink.Persist(t, snap); err != nil {
			httperr.Internal(w, r, "could not persist agent snapshot")
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(AgentSnapshotResponse{
		Accepted:     true,
		BatchID:      snap.BatchID,
		WorkloadsObs: len(snap.Workloads),
		EventsObs:    len(snap.Events),
	})
}
