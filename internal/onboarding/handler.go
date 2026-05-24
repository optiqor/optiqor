package onboarding

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// Handler requires tenant context (Phase 2: X-Optiqor-Tenant middleware;
// Phase 5+: JWT extractor).
type Handler struct {
	Service *Service
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/onboarding/state", h.GetState)
	mux.HandleFunc("POST /v1/onboarding/transition", h.Transition)
}

type StateResponse struct {
	Current          Stage              `json:"current"`
	ReachedAt        map[Stage]string   `json:"reached_at"`
	ProgressPercent  int                `json:"progress_percent"`
	Activated        bool               `json:"activated"`
	ActivationWindow string             `json:"activation_window"`
	TimeToReceipt    *DurationFormatted `json:"time_to_first_receipt,omitempty"`
	NextStage        Stage              `json:"next_stage,omitempty"`
	SLOs             SLOTable           `json:"slos"`
}

// DurationFormatted carries both fields so the dashboard does not need
// a client-side duration formatter.
type DurationFormatted struct {
	Seconds int64  `json:"seconds"`
	Label   string `json:"label"`
}

// SLOTable surfaces the Phase-5 SLO constants as humanised strings so
// the dashboard can render them without re-deriving the values.
type SLOTable struct {
	SandboxLatency        string `json:"sandbox_latency"`
	InstallToFirstPR      string `json:"install_to_first_pr"`
	InstallToFirstReco    string `json:"install_to_first_reco"`
	InstallToFirstReceipt string `json:"install_to_first_receipt"`
}

func (h *Handler) GetState(w http.ResponseWriter, r *http.Request) {
	t, err := tenancy.FromContext(r.Context())
	if err != nil {
		http.Error(w, "tenant required", http.StatusUnauthorized)
		return
	}
	st, err := h.Service.Get(r.Context(), t.TenantID)
	if err != nil {
		http.Error(w, "onboarding: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, buildStateResponse(st))
}

type TransitionRequest struct {
	To Stage `json:"to"`
}

func (h *Handler) Transition(w http.ResponseWriter, r *http.Request) {
	t, err := tenancy.FromContext(r.Context())
	if err != nil {
		http.Error(w, "tenant required", http.StatusUnauthorized)
		return
	}
	body := http.MaxBytesReader(w, r.Body, config.OnboardingTransitionMaxBytes)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req TransitionRequest
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "decode: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.To == "" {
		http.Error(w, `field "to" required`, http.StatusBadRequest)
		return
	}
	st, err := h.Service.Transition(r.Context(), t.TenantID, req.To)
	if errors.Is(err, ErrIllegalTransition) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "transition: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, buildStateResponse(st))
}

// buildStateResponse keeps GetState and Transition rendering the same
// envelope so the dashboard parses one shape.
func buildStateResponse(st State) StateResponse {
	reachedAt := make(map[Stage]string, len(st.Reached))
	for k, v := range st.Reached {
		reachedAt[k] = v.UTC().Format(time.RFC3339)
	}
	resp := StateResponse{
		Current:          st.Current,
		ReachedAt:        reachedAt,
		ProgressPercent:  st.ProgressPercent(),
		Activated:        st.Activated(SLOActivationWindow),
		ActivationWindow: SLOActivationWindow.String(),
		SLOs: SLOTable{
			SandboxLatency:        SLOSandboxLatency.String(),
			InstallToFirstPR:      SLOInstallToFirstPR.String(),
			InstallToFirstReco:    SLOInstallToFirstReco.String(),
			InstallToFirstReceipt: SLOInstallToFirstReceipt.String(),
		},
	}
	if idx := Index(st.Current); idx >= 0 && idx < len(Stages)-1 {
		resp.NextStage = Stages[idx+1]
	}
	if d, ok := st.TimeToFirstReceipt(); ok {
		resp.TimeToReceipt = &DurationFormatted{
			Seconds: int64(d.Seconds()),
			Label:   d.String(),
		}
	}
	return resp
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
