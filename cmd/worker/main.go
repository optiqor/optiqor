// Command worker runs the Optiqor Temporal worker. Phase 1 uses an
// in-memory dispatcher; Phase 3 swaps the Dispatcher implementation
// for the Temporal SDK adapter without touching call sites.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/platform/logging"
	"github.com/optiqor/optiqor/internal/worker"
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
	if err := registerWorkflows(dispatcher, logger); err != nil {
		logger.Error("workflow registration failed", "err", err)
		return 1
	}

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
