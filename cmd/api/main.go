// Command api is the Sevro HTTP API server.
//
// It serves the GitHub App webhook receiver, sandbox endpoints, and customer
// dashboard API. Real handler implementations land in Phase 1+ (see todo.md).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	addr := flag.String("addr", envOr("SEVRO_HTTP_ADDR", ":8080"), "HTTP listen address")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		// TODO(phase-1): check DB + Redis + Temporal connectivity.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		logger.Info("api listening", "addr", *addr, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen failed", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	logger.Info("api shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "err", err)
		// Cancel explicitly before exit so the deferred cancel is a safety net only.
		shutdownCancel()
		os.Exit(1) //nolint:gocritic // exitAfterDefer: shutdownCancel called explicitly above
	}
	logger.Info("api stopped")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
