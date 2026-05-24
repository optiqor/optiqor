// Command agent is the in-cluster Optiqor agent. Apache 2.0 (see
// ../../LICENSE-agent) — regulated customers won't run closed-source
// binaries in prod clusters, so this binary stays independently
// auditable.
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

	logger.Info("agent starting", "version", version)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// TODO(phase-5): client-go informer setup, Prometheus scrape, mTLS to SaaS.
	logger.Info("agent idle (watch loop not yet implemented)")
	<-ctx.Done()

	logger.Info("agent stopped")
}
