// Command api is the Optiqor HTTP API server.
//
// It serves the GitHub App webhook receiver, sandbox endpoints, and
// customer dashboard API. Phase 1 wires config + structured logging +
// readiness checks + graceful shutdown; concrete handlers land in
// later phases.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/platform/healthz"
	"github.com/optiqor/optiqor/internal/platform/logging"
	"github.com/optiqor/optiqor/internal/platform/telemetry"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/vcs"
)

var version = "dev"

func main() {
	if code := run(); code != 0 {
		os.Exit(code)
	}
}

func run() int {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return 0
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}

	logger := logging.New(os.Stdout, cfg.LogLevel)
	slog.SetDefault(logger)

	checks := healthz.NewRegistry()
	checks.Register("self", healthz.AlwaysOK)
	// Future phases register: postgres, redis, temporal, anthropic.

	metrics := telemetry.NewRegistry()
	httpRequests := metrics.NewCounter("optiqor_http_requests_total",
		"Total HTTP requests served by the api binary, by route and status",
		nil)
	httpLatency := metrics.NewHistogram("optiqor_http_request_duration_seconds",
		"HTTP request latency in seconds",
		nil,
		[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5})

	mux := buildMux(checks, logger, []byte(cfg.GitHubAppWebhookSecret), metrics)
	mountDomainRoutes(mux, buildDomainDeps())
	mux.HandleFunc("GET /v1/meta", metaHandler)
	handler := http.Handler(mux)
	handler = withAccessLog(logger, httpRequests, httpLatency, handler)
	handler = withPanicRecovery(logger, handler)
	handler = withRequestID(handler)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	listenErr := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", cfg.HTTPAddr, "version", version, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
			cancel()
			return
		}
		listenErr <- nil
	}()

	select {
	case <-ctx.Done():
	case err := <-listenErr:
		if err != nil {
			logger.Error("listen failed", "err", err)
			return 1
		}
	}

	logger.Info("api shutting down", "grace", cfg.ShutdownGrace)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "err", err)
		return 1
	}
	logger.Info("api stopped")
	return 0
}

// buildMux returns the HTTP routes the api serves. Exposed so tests can
// hit handlers without spinning a real socket.
//
// webhookSecret is the GitHub App secret used to verify inbound webhook
// signatures. Empty in dev; required in prod (config.Validate enforces).
//
// metrics is the Prometheus registry exposed under /metrics; callers
// wishing to skip the /metrics endpoint may pass nil.
func buildMux(checks *healthz.Registry, logger *slog.Logger, webhookSecret []byte, metrics *telemetry.Registry) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		results, ok := checks.Run(r.Context(), 2*time.Second)
		status := http.StatusOK
		if !ok {
			status = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      ok,
			"checks":  results,
			"version": version,
		})
	})

	if metrics != nil {
		mux.Handle("GET /metrics", metrics.Handler())
	}

	// GitHub OAuth callback. Phase 5 wires the full code-exchange +
	// session-issuance flow; Phase 1 records the (state, code) pair
	// to the structured log and returns a deterministic ack so the
	// app's redirect URI is reachable during onboarding.
	mux.HandleFunc("GET /oauth/github/callback", func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing ?code", http.StatusBadRequest)
			return
		}
		// We never log the raw code; only that one was received and
		// the state token (used to bind the redirect to the originator).
		logger.InfoContext(r.Context(), "github oauth callback",
			"state_len", len(state),
			"code_len", len(code),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"phase":  "1",
			"note":   "session issuance lands in Phase 5",
		})
	})

	// pprof endpoints — gated on the OPTIQOR_ADMIN_TOKEN header to
	// avoid exposing them to unauthenticated traffic. Empty token
	// disables pprof entirely (the safe default in dev).
	if os.Getenv("OPTIQOR_ADMIN_TOKEN") != "" {
		mountPProf(mux, os.Getenv("OPTIQOR_ADMIN_TOKEN"))
	}

	// GitHub App webhook receiver: HMAC-verifies the signature and
	// (in later phases) hands off to a Temporal workflow. Phase 1
	// returns 202 with a stable ack body so the GitHub App can be
	// installed and reach a healthy endpoint during onboarding.
	gh := vcs.NewGitHub()
	mux.HandleFunc("POST /webhooks/github", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, config.GitHubWebhookMaxBytes))
		if err != nil {
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}
		signature := r.Header.Get("X-Hub-Signature-256")
		if len(webhookSecret) == 0 {
			// Dev mode: do not verify, but log loudly so a misconfigured
			// prod doesn't accidentally accept unsigned events.
			logger.WarnContext(r.Context(), "github webhook signature not verified (dev mode)")
		} else if err := gh.VerifyWebhook(webhookSecret, signature, body); err != nil {
			logger.WarnContext(r.Context(), "github webhook rejected", "err", err)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		event := r.Header.Get("X-GitHub-Event")
		delivery := r.Header.Get("X-GitHub-Delivery")
		logger.InfoContext(r.Context(), "github webhook accepted",
			"event", event, "delivery", delivery, "bytes", len(body))
		// TODO(phase-4): start a Temporal workflow keyed off (event, delivery).
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":   "accepted",
			"event":    event,
			"delivery": delivery,
		})
	})

	return mux
}

// tenantMiddleware extracts the tenant scope from a request and stashes
// it in the context so downstream handlers can call tenancy.FromContext
// without parsing headers themselves.
//
// Phase 1: tenant id comes from the `X-Optiqor-Tenant` header. Real auth
// (JWT-extracted tenant claim) lands in Phase 5 alongside GitHub OAuth;
// the extractor function is pluggable so the middleware itself doesn't
// change when auth lands.
type TenantExtractor func(r *http.Request) (tenancy.Context, error)

// HeaderTenantExtractor returns the Phase-1 dev extractor that reads
// X-Optiqor-Tenant. Public endpoints (/healthz, /readyz, /webhooks/*)
// must be routed AROUND this middleware.
func HeaderTenantExtractor(r *http.Request) (tenancy.Context, error) {
	id := r.Header.Get("X-Optiqor-Tenant")
	if id == "" {
		return tenancy.Context{}, tenancy.ErrNoTenant
	}
	return tenancy.Context{
		TenantID:    id,
		WorkspaceID: r.Header.Get("X-Optiqor-Workspace"),
		ClusterID:   r.Header.Get("X-Optiqor-Cluster"),
		Namespace:   r.Header.Get("X-Optiqor-Namespace"),
	}, nil
}

// requireTenant wraps an http.Handler so every request reaching it has
// a validated tenant scope in its context.
func requireTenant(extract TenantExtractor, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, err := extract(r)
		if err != nil {
			http.Error(w, "tenant required", http.StatusUnauthorized)
			return
		}
		if err := t.Validate(); err != nil {
			http.Error(w, "invalid tenant", http.StatusUnauthorized)
			return
		}
		ctx := tenancy.WithContext(r.Context(), t)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// withRequestID assigns or echoes an X-Request-ID header and stashes the
// id in the request context so logs/traces can join on it.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := logging.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// fall back to time-based id; never block requests on entropy.
		return fmt.Sprintf("t-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// withPanicRecovery converts a panicking handler into a 500 response
// with a structured-log entry that includes the stack trace. The
// request continues; the surrounding server keeps serving.
func withPanicRecovery(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			logger.ErrorContext(r.Context(), "panic recovered",
				"err", fmt.Sprintf("%v", rec),
				"path", r.URL.Path,
				"method", r.Method,
				"stack", string(debug.Stack()),
			)
			// Best-effort write: if the handler already wrote headers
			// the client is hosed, but we still log. WriteHeader on a
			// committed response is a no-op + warning in stdlib.
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

// withAccessLog wraps an http.Handler with structured access logging
// and Prometheus metrics. Every request emits one info-level slog
// record carrying method, path, status, duration, bytes, and the
// request id. Sub-200ms requests are logged at debug for noise control
// when the api is healthy.
func withAccessLog(logger *slog.Logger, requests telemetry.Counter, latency telemetry.Histogram, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recordingWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		dur := time.Since(start)

		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400:
			level = slog.LevelWarn
		case dur < 200*time.Millisecond:
			level = slog.LevelDebug
		}
		logger.Log(r.Context(), level, "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", dur.Milliseconds(),
		)

		if requests != nil {
			requests.Inc()
		}
		if latency != nil {
			latency.Observe(dur.Seconds())
		}
	})
}

// mountPProf wires the standard pprof handlers behind a constant-time
// header check. Production deploys keep OPTIQOR_ADMIN_TOKEN long and
// rotated; the operator fetches profiles via:
//
//	curl -H "X-Admin-Token: $TOKEN" https://api.optiqor.dev/debug/pprof/heap > heap.pb
func mountPProf(mux *http.ServeMux, token string) {
	require := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("X-Admin-Token")
			// Constant-time compare avoids timing-side-channel leakage
			// when the token is wrong.
			if subtleConstantTimeEq(got, token) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			h.ServeHTTP(w, r)
		})
	}
	mux.Handle("GET /debug/pprof/", require(http.HandlerFunc(pprofIndex)))
	mux.Handle("GET /debug/pprof/cmdline", require(http.HandlerFunc(pprofCmdline)))
	mux.Handle("GET /debug/pprof/profile", require(http.HandlerFunc(pprofProfile)))
	mux.Handle("GET /debug/pprof/symbol", require(http.HandlerFunc(pprofSymbol)))
	mux.Handle("GET /debug/pprof/trace", require(http.HandlerFunc(pprofTrace)))
}

// recordingWriter intercepts the status code and byte count without
// changing the streaming behaviour of the underlying ResponseWriter.
type recordingWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *recordingWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *recordingWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}
