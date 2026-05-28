package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/platform/httperr"
	"github.com/optiqor/optiqor/internal/tenancy"
)

// CookieName is set by /v1/session/issue, separate from the Auth.js
// session cookie web/ manages.
const CookieName = "optiqor_session"

// SessionTTL is short on purpose: the dashboard re-issues against its
// rotating Auth.js session.
const SessionTTL = 12 * time.Hour

type Handler struct {
	Signer *Signer
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/session/whoami", h.Whoami)
	mux.HandleFunc("POST /v1/session/issue", h.Issue)
}

// WhoamiResponse carries source so the dashboard can tell the JWT path
// from the Phase-2 header fallback. Phase 5 retires the header source.
type WhoamiResponse struct {
	TenantID    string `json:"tenant_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Subject     string `json:"subject,omitempty"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source"` // "jwt" | "header"
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// Whoami returns 401 if no JWT or tenant context is present.
func (h *Handler) Whoami(w http.ResponseWriter, r *http.Request) {
	if s, ok := h.sessionFromRequest(r); ok {
		writeJSON(w, http.StatusOK, WhoamiResponse{
			TenantID:    s.TenantID,
			WorkspaceID: s.WorkspaceID,
			Subject:     s.Subject,
			Name:        s.Name,
			Source:      "jwt",
			ExpiresAt:   s.ExpiresAt.Format(time.RFC3339),
		})
		return
	}
	if t, err := tenancy.FromContext(r.Context()); err == nil {
		writeJSON(w, http.StatusOK, WhoamiResponse{
			TenantID:    t.TenantID,
			WorkspaceID: t.WorkspaceID,
			Source:      "header",
		})
		return
	}
	httperr.Unauthorized(w, r, "no session — sign in to issue one via POST /v1/session/issue")
}

// IssueRequest is the dashboard-built handshake body. Subject is the
// Auth.js stable identity (provider id or verified email); the dashboard
// validates it server-side before forwarding. Phase 5 binds the issuer
// to a verified provider callback so this can fail closed.
type IssueRequest struct {
	Subject     string `json:"subject"`
	Name        string `json:"name,omitempty"`
	TenantID    string `json:"tenant_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type IssueResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

func (h *Handler) Issue(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, config.SessionIssueMaxBytes)
	defer func() { _ = r.Body.Close() }()

	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req IssueRequest
	if err := dec.Decode(&req); err != nil {
		if httperr.IsBodyTooLarge(err) {
			httperr.BodyTooLarge(w, r, config.SessionIssueMaxBytes)
			return
		}
		httperr.InvalidJSON(w, r, err)
		return
	}
	if req.Subject == "" {
		httperr.MissingField(w, r, "subject")
		return
	}
	if req.TenantID == "" {
		httperr.MissingField(w, r, "tenant_id")
		return
	}

	now := h.Signer.now()
	s := Session{
		TenantID:    req.TenantID,
		WorkspaceID: req.WorkspaceID,
		Subject:     req.Subject,
		Name:        req.Name,
		IssuedAt:    now,
		ExpiresAt:   now.Add(SessionTTL),
	}
	token, err := h.Signer.Issue(s)
	if err != nil {
		httperr.Internal(w, r, "could not issue session")
		return
	}

	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure gated on r.TLS so dev/integration over HTTP works; prod is HTTPS
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  s.ExpiresAt,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, IssueResponse{
		Token:     token,
		ExpiresAt: s.ExpiresAt.Format(time.RFC3339),
	})
}

// sessionFromRequest tries Authorization: Bearer first, then the cookie.
func (h *Handler) sessionFromRequest(r *http.Request) (Session, bool) {
	if h.Signer == nil {
		return Session{}, false
	}
	if bearer := r.Header.Get("Authorization"); strings.HasPrefix(bearer, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(bearer, "Bearer "))
		if s, err := h.Signer.Verify(token); err == nil {
			return s, true
		} else if !errors.Is(err, ErrInvalidToken) {
			return Session{}, false
		}
	}
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		if s, err := h.Signer.Verify(c.Value); err == nil {
			return s, true
		}
	}
	return Session{}, false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
