// Command agent is the in-cluster Optiqor agent.
//
// Runs inside customer Kubernetes clusters. Reads K8s API via client-go
// informers and scrapes Prometheus, then ships data over mTLS to the Optiqor
// SaaS using short-lived JWTs.
//
// Licensed under Apache 2.0 (see ../../LICENSE-agent). Regulated customers
// will not run closed-source binaries in production clusters; this binary
// must remain independently auditable.
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
