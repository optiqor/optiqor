// Command api is the Sevro HTTP API server.
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
	"syscall"
	"time"

	"github.com/lowplane/backend/internal/platform/config"
	"github.com/lowplane/backend/internal/platform/healthz"
	"github.com/lowplane/backend/internal/platform/logging"
	"github.com/lowplane/backend/internal/tenancy"
	"github.com/lowplane/backend/internal/vcs"
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

	mux := buildMux(checks, logger, []byte(cfg.GitHubAppWebhookSecret))
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           withRequestID(mux),
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
func buildMux(checks *healthz.Registry, logger *slog.Logger, webhookSecret []byte) *http.ServeMux {
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

	// GitHub App webhook receiver: HMAC-verifies the signature and
	// (in later phases) hands off to a Temporal workflow. Phase 1
	// returns 202 with a stable ack body so the GitHub App can be
	// installed and reach a healthy endpoint during onboarding.
	gh := vcs.NewGitHub()
	mux.HandleFunc("POST /webhooks/github", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20)) // 8 MiB
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
// Phase 1: tenant id comes from the `X-Sevro-Tenant` header. Real auth
// (JWT-extracted tenant claim) lands in Phase 5 alongside GitHub OAuth;
// the extractor function is pluggable so the middleware itself doesn't
// change when auth lands.
type TenantExtractor func(r *http.Request) (tenancy.Context, error)

// HeaderTenantExtractor returns the Phase-1 dev extractor that reads
// X-Sevro-Tenant. Public endpoints (/healthz, /readyz, /webhooks/*)
// must be routed AROUND this middleware.
func HeaderTenantExtractor(r *http.Request) (tenancy.Context, error) {
	id := r.Header.Get("X-Sevro-Tenant")
	if id == "" {
		return tenancy.Context{}, tenancy.ErrNoTenant
	}
	return tenancy.Context{
		TenantID:    id,
		WorkspaceID: r.Header.Get("X-Sevro-Workspace"),
		ClusterID:   r.Header.Get("X-Sevro-Cluster"),
		Namespace:   r.Header.Get("X-Sevro-Namespace"),
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
