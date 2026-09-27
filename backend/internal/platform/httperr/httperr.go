// Package httperr renders structured JSON error responses. Every
// handler error goes through this package so integrators see a
// consistent shape:
//
//	{ "error": { "code": "REQUEST_BODY_TOO_LARGE", "message": "…",
//	             "status_code": 413, "request_id": "…" } }
//
// The shape, codes, and content-type are stable surfaces — break them
// in an ADR, not in a PR.
package httperr

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/optiqor/optiqor/internal/platform/logging"
)

// Error is the wire shape. RequestID echoes the inbound X-Request-ID so
// customers can quote it in support tickets. Details is open-ended:
// rate-limit handlers attach quota state, validation handlers attach
// the offending field name.
type Error struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	StatusCode int            `json:"status_code"`
	RequestID  string         `json:"request_id,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// Envelope wraps Error so the body is always `{"error": {...}}` —
// makes client-side type narrowing trivial in TypeScript/Python.
type Envelope struct {
	Error Error `json:"error"`
}

// Canonical error codes. Add new ones here so the docs site can list
// them; avoid handler-local strings.
const (
	CodeBadRequest          = "BAD_REQUEST"
	CodeInvalidJSON         = "INVALID_JSON"
	CodeUnknownField        = "UNKNOWN_FIELD"
	CodeMissingField        = "MISSING_FIELD"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeForbidden           = "FORBIDDEN"
	CodeNotFound            = "NOT_FOUND"
	CodeGone                = "GONE"
	CodeMethodNotAllowed    = "METHOD_NOT_ALLOWED"
	CodeRequestBodyTooLarge = "REQUEST_BODY_TOO_LARGE"
	CodeRateLimited         = "RATE_LIMITED"
	CodeConflict            = "CONFLICT"
	CodeInternalError       = "INTERNAL_ERROR"
	CodeUpstreamError       = "UPSTREAM_ERROR"
	CodeServiceUnavailable  = "SERVICE_UNAVAILABLE"
)

// Write renders the envelope. It also sets X-Request-ID on the
// response header so curl users see it without parsing the body.
func Write(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteWithDetails(w, r, status, code, message, nil)
}

// WriteWithDetails is Write plus a free-form details map (quota state,
// validation field name). Use sparingly: schema changes here are
// surface-breaking for integrators.
func WriteWithDetails(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	reqID := logging.RequestIDFromContext(r.Context())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if reqID != "" {
		w.Header().Set("X-Request-ID", reqID)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{Error: Error{
		Code:       code,
		Message:    message,
		StatusCode: status,
		RequestID:  reqID,
		Details:    details,
	}})
}

// BadRequest400 is the catch-all 400. Prefer InvalidJSON / MissingField
// when the cause is specific.
func BadRequest(w http.ResponseWriter, r *http.Request, msg string) {
	Write(w, r, http.StatusBadRequest, CodeBadRequest, msg)
}

func InvalidJSON(w http.ResponseWriter, r *http.Request, err error) {
	msg := "request body is not valid JSON"
	if err != nil {
		// Strip raw parser internals; the position info is helpful, the
		// stdlib type names ("json.SyntaxError") are not.
		msg = "request body is not valid JSON: " + redactDecoderError(err)
	}
	Write(w, r, http.StatusBadRequest, CodeInvalidJSON, msg)
}

func MissingField(w http.ResponseWriter, r *http.Request, field string) {
	WriteWithDetails(w, r, http.StatusBadRequest, CodeMissingField,
		fmt.Sprintf("required field %q is missing", field),
		map[string]any{"field": field})
}

func Unauthorized(w http.ResponseWriter, r *http.Request, msg string) {
	if msg == "" {
		msg = "tenant context required"
	}
	Write(w, r, http.StatusUnauthorized, CodeUnauthorized, msg)
}

func Forbidden(w http.ResponseWriter, r *http.Request, msg string) {
	Write(w, r, http.StatusForbidden, CodeForbidden, msg)
}

func NotFound(w http.ResponseWriter, r *http.Request, resource string) {
	Write(w, r, http.StatusNotFound, CodeNotFound, resource+" not found")
}

func Gone(w http.ResponseWriter, r *http.Request, resource string) {
	Write(w, r, http.StatusGone, CodeGone, resource+" expired")
}

func MethodNotAllowed(w http.ResponseWriter, r *http.Request, allowed string) {
	w.Header().Set("Allow", allowed)
	Write(w, r, http.StatusMethodNotAllowed, CodeMethodNotAllowed,
		"method "+r.Method+" not allowed; expected "+allowed)
}

func BodyTooLarge(w http.ResponseWriter, r *http.Request, maxBytes int64) {
	WriteWithDetails(w, r, http.StatusRequestEntityTooLarge, CodeRequestBodyTooLarge,
		fmt.Sprintf("request body exceeds %d bytes", maxBytes),
		map[string]any{"max_bytes": maxBytes})
}

func Conflict(w http.ResponseWriter, r *http.Request, msg string) {
	Write(w, r, http.StatusConflict, CodeConflict, msg)
}

func Internal(w http.ResponseWriter, r *http.Request, msg string) {
	if msg == "" {
		msg = "internal server error"
	}
	Write(w, r, http.StatusInternalServerError, CodeInternalError, msg)
}

func Upstream(w http.ResponseWriter, r *http.Request, msg string) {
	Write(w, r, http.StatusBadGateway, CodeUpstreamError, msg)
}

func ServiceUnavailable(w http.ResponseWriter, r *http.Request, msg string) {
	Write(w, r, http.StatusServiceUnavailable, CodeServiceUnavailable, msg)
}

// LogAndInternal logs the underlying error then renders a generic 500.
// Use when the caller error reveals internal state customers should
// not see (DB connection strings, file paths).
func LogAndInternal(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error, where string) {
	if log != nil {
		log.ErrorContext(r.Context(), "handler error", "where", where, "err", err)
	}
	Internal(w, r, "")
}

// IsBodyTooLarge tests whether a MaxBytesReader trip produced err.
// http.MaxBytesReader returns a *http.MaxBytesError on size violation.
func IsBodyTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

func redactDecoderError(err error) string {
	// Trim wrapper noise but keep the parser's position info — it's
	// what makes "missing comma at offset 47" actionable.
	s := err.Error()
	const noisy = "json: "
	if len(s) > len(noisy) && s[:len(noisy)] == noisy {
		s = s[len(noisy):]
	}
	return s
}
