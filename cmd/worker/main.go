// Command worker runs the Sevro Temporal worker.
//
// Workflows: PR analysis, Apply Fix generation, Receipt issuance, Auto-Rollback
// monitoring, Cost Spike detection. Real workflow implementations land in
// Phase 1+ (see todo.md).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	logger.Info("worker starting", "version", version)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// TODO(phase-1): connect Temporal client, register workflows + activities,
	// start per-tenant queue controller.
	logger.Info("worker idle (no workflows registered yet)")
	<-ctx.Done()

	logger.Info("worker stopped")
}
