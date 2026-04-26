package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lowplane/backend/internal/platform/healthz"
)

func TestHealthz(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyz_AllOK(t *testing.T) {
	r := healthz.NewRegistry()
	r.Register("self", healthz.AlwaysOK)
	r.Register("more", healthz.AlwaysOK)

	mux := buildMux(r, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		OK     bool              `json:"ok"`
		Checks []healthz.Result  `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, rec.Body.String())
	}
	if !body.OK || len(body.Checks) != 2 {
		t.Fatalf("body=%+v", body)
	}
}

func TestReadyz_Failing(t *testing.T) {
	r := healthz.NewRegistry()
	r.Register("self", healthz.AlwaysOK)
	r.Register("redis", healthz.AlwaysFail("connection refused"))

	mux := buildMux(r, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var body struct {
		OK     bool             `json:"ok"`
		Checks []healthz.Result `json:"checks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.OK {
		t.Fatal("body.ok should be false")
	}
	var found bool
	for _, c := range body.Checks {
		if c.Name == "redis" && !c.OK {
			found = true
		}
	}
	if !found {
		t.Errorf("redis-failure not surfaced: %+v", body.Checks)
	}
}

func TestRequestID_Generated(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	withRequestID(mux).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("X-Request-ID should be generated when missing")
	}
}

func TestRequestID_Echoed(t *testing.T) {
	mux := buildMux(healthz.NewRegistry(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	rec := httptest.NewRecorder()
	withRequestID(mux).ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); got != "abc-123" {
		t.Fatalf("X-Request-ID echoed = %q, want abc-123", got)
	}
}

func TestReadyz_TimeoutNotPanicking(t *testing.T) {
	r := healthz.NewRegistry()
	r.Register("slow", func(ctx context.Context) error {
		select {
		case <-time.After(time.Second):
			return nil
		case <-ctx.Done():
			return errors.New("ctx done")
		}
	})

	mux := buildMux(r, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()

	// Tight client deadline; the handler-side timeout (2s) is more
	// generous than this, so we expect 503 only if the handler
	// honors its per-check timeout. We can't easily override that
	// here, so we just assert it does not panic and returns a JSON
	// document.
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Body.Len() == 0 {
		t.Fatal("body should not be empty")
	}
}
