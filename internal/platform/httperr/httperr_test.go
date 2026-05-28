package httperr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/platform/logging"
)

func newReq(reqID string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/x", http.NoBody)
	if reqID != "" {
		r = r.WithContext(logging.WithRequestID(r.Context(), reqID))
	}
	return r
}

func decode(t *testing.T, w *httptest.ResponseRecorder) Envelope {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, w.Body.String())
	}
	return env
}

func TestWrite_SetsJSONShapeAndRequestID(t *testing.T) {
	w := httptest.NewRecorder()
	Write(w, newReq("req-42"), http.StatusBadRequest, CodeBadRequest, "bad input")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q", got)
	}
	if got := w.Header().Get("X-Request-ID"); got != "req-42" {
		t.Errorf("X-Request-ID = %q", got)
	}
	env := decode(t, w)
	if env.Error.Code != CodeBadRequest || env.Error.Message != "bad input" ||
		env.Error.StatusCode != 400 || env.Error.RequestID != "req-42" {
		t.Errorf("envelope = %+v", env)
	}
}

func TestBodyTooLarge_IncludesMaxInDetails(t *testing.T) {
	w := httptest.NewRecorder()
	BodyTooLarge(w, newReq(""), 1<<20)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d", w.Code)
	}
	env := decode(t, w)
	if env.Error.Details["max_bytes"].(float64) != float64(1<<20) {
		t.Errorf("max_bytes detail missing: %+v", env.Error.Details)
	}
	if !strings.Contains(env.Error.Message, "1048576 bytes") {
		t.Errorf("message must quote the cap:\n%s", env.Error.Message)
	}
}

func TestMethodNotAllowed_SetsAllowHeader(t *testing.T) {
	w := httptest.NewRecorder()
	MethodNotAllowed(w, newReq(""), "POST")
	if got := w.Header().Get("Allow"); got != "POST" {
		t.Errorf("Allow = %q", got)
	}
}

func TestGone_StatusAndCode(t *testing.T) {
	w := httptest.NewRecorder()
	Gone(w, newReq(""), "shared analysis")
	if w.Code != http.StatusGone {
		t.Errorf("status = %d, want 410", w.Code)
	}
	env := decode(t, w)
	if env.Error.Code != CodeGone {
		t.Errorf("code = %q", env.Error.Code)
	}
}

func TestRequestIDFromContext_FallsBackToEmpty(t *testing.T) {
	if got := logging.RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("empty ctx = %q", got)
	}
}

func TestIsBodyTooLarge_DetectsMaxBytesError(t *testing.T) {
	if IsBodyTooLarge(errors.New("nope")) {
		t.Error("non-MaxBytesError reported as body too large")
	}
	if !IsBodyTooLarge(&http.MaxBytesError{Limit: 1}) {
		t.Error("MaxBytesError not detected")
	}
}
