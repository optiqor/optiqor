package preflight

import (
	"encoding/json"
	"net/http"

	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/platform/httperr"
)

// Handler serves POST /v1/onboarding/preflight. The request body
// carries the customer-supplied Config — what their cluster's
// Prometheus Service is named, primarily. The response is the
// check list the dashboard preview renders.
type Handler struct {
	Runner *Runner
}

// Request mirrors Config field-for-field so the JSON body decodes
// directly into a Config the runner accepts.
type Request = Config

type Response struct {
	Checks []Check `json:"checks"`
}

func (h *Handler) Run(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httperr.MethodNotAllowed(w, r, "POST")
		return
	}
	if h.Runner == nil {
		httperr.Internal(w, r, "preflight runner not configured")
		return
	}
	body := http.MaxBytesReader(w, r.Body, config.OnboardingTransitionMaxBytes)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req Request
	// Empty body is OK — caller may just want to probe the defaults.
	if r.ContentLength > 0 {
		if err := dec.Decode(&req); err != nil {
			if httperr.IsBodyTooLarge(err) {
				httperr.BodyTooLarge(w, r, config.OnboardingTransitionMaxBytes)
				return
			}
			httperr.InvalidJSON(w, r, err)
			return
		}
	}
	checks, err := h.Runner.Run(r.Context(), req)
	if err != nil {
		httperr.Internal(w, r, "preflight: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Response{Checks: checks})
}
