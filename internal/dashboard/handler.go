// Package dashboard serves the /v1/savings/summary, /v1/apply-fixes,
// and /v1/agent/health endpoints the Next.js dashboard reads. Every
// endpoint is tenant-scoped through the requireTenant middleware in
// cmd/api, so handlers read tenancy.FromContext fail-closed.
package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/optiqor/optiqor/internal/platform/httperr"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// SavingsSource projects merged Apply Fixes per period. Backed by
// PgStore in production; tests inject a deterministic source. Returns
// cents to stay precision-stable across JSON rounds.
type SavingsSource interface {
	Summary(ctx tenancy.Context) (SavingsSummary, error)
}

type SavingsSummary struct {
	LifetimeCents int64 `json:"lifetime_cents"`
	MTDCents      int64 `json:"month_to_date_cents"`
	YTDCents      int64 `json:"year_to_date_cents"`
	MergedCount   int   `json:"merged_count"`
}

// ApplyFixesSource lists the tenant's apply_fixes filtered by state.
type ApplyFixesSource interface {
	List(ctx tenancy.Context, state string, limit int) ([]ApplyFix, error)
}

type ApplyFix struct {
	ID              string     `json:"id"`
	Repo            string     `json:"repo"`
	PRURL           string     `json:"pr_url"`
	State           string     `json:"state"`
	MonthlyUSDCents int64      `json:"monthly_usd_cents"`
	OpenedAt        time.Time  `json:"opened_at"`
	MergedAt        *time.Time `json:"merged_at,omitempty"`
}

// AgentHealthSource reads tenants.agents and projects the freshest
// row's status. Returns AgentHealth even when no agents are
// registered (StatusOffline) so the dashboard can render "no agent
// installed" without a special-case error path.
type AgentHealthSource interface {
	Latest(ctx tenancy.Context) (AgentHealth, error)
}

type AgentHealth struct {
	Status               string    `json:"status"`
	LastCheckin          time.Time `json:"last_checkin"`
	DataFreshnessSeconds int       `json:"data_freshness_seconds"`
	Version              string    `json:"version,omitempty"`
}

type Handler struct {
	Savings    SavingsSource
	ApplyFixes ApplyFixesSource
	Agent      AgentHealthSource
	// Now is injected so handlers + tests share a clock. Defaults to
	// time.Now when nil; production wires the same clock the
	// onboarding handler uses for SLO calculations.
	Now func() time.Time
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/savings/summary", h.Summary)
	mux.HandleFunc("GET /v1/apply-fixes", h.ListApplyFixes)
	mux.HandleFunc("GET /v1/agent/health", h.AgentHealth)
}

func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	t, err := tenancy.FromContext(r.Context())
	if err != nil {
		httperr.Unauthorized(w, r, "")
		return
	}
	if h.Savings == nil {
		writeJSON(w, SavingsSummary{})
		return
	}
	out, err := h.Savings.Summary(t)
	if err != nil {
		httperr.Internal(w, r, "could not compute savings summary")
		return
	}
	writeJSON(w, out)
}

func (h *Handler) ListApplyFixes(w http.ResponseWriter, r *http.Request) {
	t, err := tenancy.FromContext(r.Context())
	if err != nil {
		httperr.Unauthorized(w, r, "")
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	if !validState(state) {
		httperr.WriteWithDetails(w, r, http.StatusBadRequest, httperr.CodeBadRequest,
			"invalid state filter",
			map[string]any{"allowed": []string{"open", "merged", "closed", "rolled-back"}})
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if h.ApplyFixes == nil {
		writeJSON(w, applyFixesPage{Items: []ApplyFix{}})
		return
	}
	items, err := h.ApplyFixes.List(t, state, limit)
	if err != nil {
		httperr.Internal(w, r, "could not list apply fixes")
		return
	}
	if items == nil {
		items = []ApplyFix{}
	}
	writeJSON(w, applyFixesPage{Items: items})
}

func (h *Handler) AgentHealth(w http.ResponseWriter, r *http.Request) {
	t, err := tenancy.FromContext(r.Context())
	if err != nil {
		httperr.Unauthorized(w, r, "")
		return
	}
	if h.Agent == nil {
		writeJSON(w, AgentHealth{Status: "offline"})
		return
	}
	out, err := h.Agent.Latest(t)
	if err != nil {
		httperr.Internal(w, r, "could not read agent health")
		return
	}
	writeJSON(w, out)
}

type applyFixesPage struct {
	Items []ApplyFix `json:"items"`
}

func validState(s string) bool {
	switch s {
	case "open", "merged", "closed", "rolled-back":
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}
