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
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lowplane/backend/internal/platform/config"
	"github.com/lowplane/backend/internal/platform/healthz"
	"github.com/lowplane/backend/internal/platform/logging"
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

	mux := buildMux(checks, logger)
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
func buildMux(checks *healthz.Registry, logger *slog.Logger) *http.ServeMux {
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

	return mux
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
