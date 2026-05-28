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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

	pool, err := openPool(cfg, logger)
	if err != nil {
		logger.Error("pool init failed", "err", err)
		return 1
	}
	if pool != nil {
		defer pool.Close()
	}

	dispatcher := worker.NewInMemory()
	if err := registerWorkflows(dispatcher, logger, cfg, pool); err != nil {
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

// openPool returns a pgxpool when cfg.PostgresDSN is set. Dev mode
// without a DSN returns (nil, nil); workflows fall back to in-memory
// stores and the LLM recorder skips writes.
func openPool(cfg config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	if cfg.PostgresDSN == "" {
		log.Warn("no OPTIQOR_POSTGRES_DSN — RLS-bound writes disabled")
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgx ping: %w", err)
	}
	log.Info("postgres connected", "max_conns", pool.Config().MaxConns)
	return pool, nil
}
