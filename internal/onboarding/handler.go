package onboarding

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/platform/httperr"
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
	mux.HandleFunc("GET /v1/onboarding/health", h.GetHealth)
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
		httperr.Unauthorized(w, r, "tenant required — set X-Optiqor-Tenant or a valid session JWT")
		return
	}
	st, err := h.Service.Get(r.Context(), t.TenantID)
	if err != nil {
		httperr.Internal(w, r, "could not load onboarding state")
		return
	}
	writeJSON(w, http.StatusOK, buildStateResponse(st))
}

// HealthResponse is the shareable funnel-position view per todo.md
// L351: tenant onboarding stage, blockers list, time-stamps, SLO
// adherence. Designed to be safe to surface to a customer-success
// teammate alongside the operator they're helping.
type HealthResponse struct {
	Current          Stage              `json:"current"`
	NextStage        Stage              `json:"next_stage,omitempty"`
	ProgressPercent  int                `json:"progress_percent"`
	Activated        bool               `json:"activated"`
	ActivationWindow string             `json:"activation_window"`
	TimeInStage      *DurationFormatted `json:"time_in_current_stage,omitempty"`
	TimeToReceipt    *DurationFormatted `json:"time_to_first_receipt,omitempty"`
	HealthyTTFR      bool               `json:"healthy_ttfr"`
	Blockers         []Blocker          `json:"blockers"`
	SLOs             SLOTable           `json:"slos"`
}

// Blocker is one actionable item the customer must close before the
// next stage transition fires. Action is a verb the dashboard surfaces
// on the CTA button; Link points the operator at the relevant doc.
type Blocker struct {
	Stage  Stage  `json:"stage"`
	Reason string `json:"reason"`
	Action string `json:"action"`
	Link   string `json:"link,omitempty"`
}

type TransitionRequest struct {
	To Stage `json:"to"`
}

func (h *Handler) Transition(w http.ResponseWriter, r *http.Request) {
	t, err := tenancy.FromContext(r.Context())
	if err != nil {
		httperr.Unauthorized(w, r, "tenant required — set X-Optiqor-Tenant or a valid session JWT")
		return
	}
	body := http.MaxBytesReader(w, r.Body, config.OnboardingTransitionMaxBytes)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req TransitionRequest
	if err := dec.Decode(&req); err != nil {
		if httperr.IsBodyTooLarge(err) {
			httperr.BodyTooLarge(w, r, config.OnboardingTransitionMaxBytes)
			return
		}
		httperr.InvalidJSON(w, r, err)
		return
	}
	if req.To == "" {
		httperr.MissingField(w, r, "to")
		return
	}
	st, err := h.Service.Transition(r.Context(), t.TenantID, req.To)
	if errors.Is(err, ErrIllegalTransition) {
		httperr.WriteWithDetails(w, r, http.StatusConflict, "ILLEGAL_TRANSITION",
			err.Error(),
			map[string]any{"current": st.Current, "requested": req.To})
		return
	}
	if err != nil {
		httperr.Internal(w, r, "could not record transition")
		return
	}
	writeJSON(w, http.StatusOK, buildStateResponse(st))
}

func (h *Handler) GetHealth(w http.ResponseWriter, r *http.Request) {
	t, err := tenancy.FromContext(r.Context())
	if err != nil {
		httperr.Unauthorized(w, r, "tenant required — set X-Optiqor-Tenant or a valid session JWT")
		return
	}
	st, err := h.Service.Get(r.Context(), t.TenantID)
	if err != nil {
		httperr.Internal(w, r, "could not load onboarding state")
		return
	}
	writeJSON(w, http.StatusOK, buildHealthResponse(st, h.Service.now()))
}

// buildHealthResponse projects the current stage + standing blockers
// for the GetHealth endpoint. Pure function for test pinning.
func buildHealthResponse(st State, now time.Time) HealthResponse {
	resp := HealthResponse{
		Current:          st.Current,
		ProgressPercent:  st.ProgressPercent(),
		Activated:        st.Activated(SLOActivationWindow),
		ActivationWindow: SLOActivationWindow.String(),
		HealthyTTFR:      st.HealthyTimeToFirstReceipt(),
		Blockers:         blockersFor(st.Current),
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
	if t, ok := st.Reached[st.Current]; ok {
		d := now.Sub(t)
		if d < 0 {
			d = 0
		}
		resp.TimeInStage = &DurationFormatted{Seconds: int64(d.Seconds()), Label: d.Truncate(time.Second).String()}
	}
	if d, ok := st.TimeToFirstReceipt(); ok {
		resp.TimeToReceipt = &DurationFormatted{Seconds: int64(d.Seconds()), Label: d.String()}
	}
	return resp
}

// blockersFor maps a stage to the canonical "what does the customer
// need to do next" prompts. Kept as a pure switch so the dashboard
// receives stable wire shapes across deployments without a config flag.
func blockersFor(s Stage) []Blocker {
	switch s {
	case StageSignedUp:
		return []Blocker{{
			Stage: StageSignedUp, Reason: "GitHub App not yet installed",
			Action: "Install the Optiqor GitHub App",
			Link:   "https://optiqor.dev/install/github",
		}}
	case StageVCSConnected:
		return []Blocker{{
			Stage: StageVCSConnected, Reason: "no repo selected for analysis",
			Action: "Pick the chart repo Optiqor should analyze",
			Link:   "https://optiqor.dev/install/repo",
		}}
	case StageRepoSelected:
		return []Blocker{{
			Stage: StageRepoSelected, Reason: "first sandbox analysis not yet run",
			Action: "Open the sandbox and analyze a values.yaml",
			Link:   "https://optiqor.dev/sandbox",
		}}
	case StageFirstPRAnalyzed:
		return []Blocker{{
			Stage: StageFirstPRAnalyzed, Reason: "agent not installed in the cluster",
			Action: "helm install optiqor-agent — the install wizard generates the values for you",
			Link:   "https://optiqor.dev/install/agent",
		}}
	case StageAgentInstalled:
		return []Blocker{{
			Stage: StageAgentInstalled, Reason: "no Apply Fix PRs merged yet",
			Action: "Review the open Apply Fix PRs and merge one",
			Link:   "https://optiqor.dev/app/apply-fixes",
		}}
	case StageFirstApplyFix:
		return []Blocker{{
			Stage: StageFirstApplyFix, Reason: "first Receipt pending — waiting for 7-day bill window",
			Action: "Watch for the Receipt in the dashboard; nothing to do",
			Link:   "https://optiqor.dev/app/receipts",
		}}
	case StageFirstReceipt:
		return nil
	default:
		return nil
	}
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
