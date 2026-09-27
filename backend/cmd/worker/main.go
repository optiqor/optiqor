// Command worker runs the Optiqor Temporal worker. Phase 1 uses an
// in-memory dispatcher; Phase 3 swaps the Dispatcher implementation
// for the Temporal SDK adapter without touching call sites.
package main

import (
	"context"
	"errors"
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
	"github.com/optiqor/optiqor/internal/platform/telemetry"
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

	if reporter, err := initSentry(cfg); err != nil {
		logger.Warn("sentry init failed; falling back to noop", "err", err)
	} else if reporter != nil {
		prev := telemetry.SetReporter(reporter)
		defer func() {
			_ = reporter.Flush(2000)
			telemetry.SetReporter(prev)
		}()
		logger.Info("sentry reporter wired", "env", cfg.SentryEnvironment, "release", cfg.SentryRelease)
	}

	pool, err := openPool(cfg, logger)
	if err != nil {
		logger.Error("pool init failed", "err", err)
		return 1
	}
	if pool != nil {
		defer pool.Close()
	}

	metrics := telemetry.NewRegistry()
	dispatcher := worker.NewInMemory()
	if err := registerWorkflows(dispatcher, logger, cfg, pool, metrics); err != nil {
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

// initSentry mirrors cmd/api/initSentry — empty DSN returns (nil, nil)
// so the worker boots without a reporter when not configured.
func initSentry(cfg config.Config) (telemetry.ErrorReporter, error) {
	if cfg.SentryDSN == "" {
		return nil, nil
	}
	env := cfg.SentryEnvironment
	if env == "" {
		env = string(cfg.Env)
	}
	rate := cfg.SentrySampleRate
	if rate <= 0 {
		rate = 1.0
	}
	r, err := telemetry.NewSentryReporter(telemetry.SentryConfig{
		DSN:         cfg.SentryDSN,
		Environment: env,
		Release:     cfg.SentryRelease,
		SampleRate:  rate,
	})
	if err != nil {
		if errors.Is(err, telemetry.ErrSentryNotConfigured) {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
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
