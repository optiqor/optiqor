// Command worker runs the Sevro Temporal worker.
//
// Workflows: PR analysis, Apply Fix generation, Receipt issuance,
// Auto-Rollback monitoring, Cost Spike detection. Phase 1 wires
// config + structured logging + an in-memory dispatcher; the
// production Temporal SDK adapter swaps in at Phase 3 by replacing
// the Dispatcher implementation — call sites do not change.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/lowplane/backend/internal/platform/config"
	"github.com/lowplane/backend/internal/platform/logging"
	"github.com/lowplane/backend/internal/worker"
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

	dispatcher := worker.NewInMemory()
	// Phase 3 will register: PR-analysis, Apply Fix, Receipt issuer,
	// Auto-Rollback monitor, Cost Spike detector. Phase 1 leaves the
	// dispatcher empty so the binary boots without external services.

	logger.Info("worker started",
		"version", version,
		"env", cfg.Env,
		"temporal", cfg.TemporalHost,
		"workflows", dispatcher.Workflows(),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	<-ctx.Done()
	logger.Info("worker draining", "grace", cfg.ShutdownGrace)

	drainCtx, drainCancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer drainCancel()
	if err := dispatcher.Drain(drainCtx); err != nil {
		logger.Error("drain failed", "err", err)
		return 1
	}
	logger.Info("worker stopped")
	return 0
}
