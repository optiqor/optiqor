// Command agent is the in-cluster Optiqor agent. Apache 2.0 (see
// ../../LICENSE-agent) — regulated customers won't run closed-source
// binaries in prod clusters, so this binary stays independently
// auditable.
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

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/optiqor/optiqor/internal/agent/cluster"
)

var version = "dev"

func main() {
	if code := run(); code != 0 {
		os.Exit(code)
	}
}

func run() int {
	showVersion := flag.Bool("version", false, "print version and exit")
	kubeconfig := flag.String("kubeconfig", "", "absolute path to a kubeconfig (out-of-cluster dev only; empty uses in-cluster config)")
	clusterID := flag.String("cluster-id", os.Getenv("OPTIQOR_CLUSTER_ID"), "stable cluster identifier; required")
	healthAddr := flag.String("health-addr", envOr("OPTIQOR_HEALTH_ADDR", ":8088"), "address for /healthz")
	resyncStr := flag.String("resync", envOr("OPTIQOR_INFORMER_RESYNC", "10m"), "informer cache resync period")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return 0
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	logger.Info("agent starting", "version", version)

	if *clusterID == "" {
		logger.Error("missing --cluster-id / OPTIQOR_CLUSTER_ID")
		return 2
	}

	resync, err := time.ParseDuration(*resyncStr)
	if err != nil {
		logger.Error("bad --resync", "value", *resyncStr, "err", err)
		return 2
	}

	cfg, err := loadK8sConfig(*kubeconfig)
	if err != nil {
		logger.Error("load kube config", "err", err)
		return 1
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Error("kubernetes clientset", "err", err)
		return 1
	}
	dynCli, err := dynamic.NewForConfig(cfg)
	if err != nil {
		logger.Error("dynamic client", "err", err)
		return 1
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	readers, err := cluster.Start(ctx, cluster.Config{
		ClusterID:    *clusterID,
		Client:       cs,
		ResyncPeriod: resync,
	})
	if err != nil {
		logger.Error("informer start", "err", err)
		return 1
	}

	// VPA + Karpenter are CRD-conditional. Skipping is non-fatal —
	// validator nil-checks both signals.
	vpa, err := cluster.NewVPAReader(dynCli, cs.Discovery(), *clusterID)
	if err != nil {
		logger.Warn("vpa reader init failed", "err", err)
	} else if vpa == nil {
		logger.Info("vpa CRD absent; skipping reader")
	}
	karpenter, err := cluster.NewKarpenterReader(dynCli, cs.Discovery(), *clusterID)
	if err != nil {
		logger.Warn("karpenter reader init failed", "err", err)
	} else if karpenter == nil {
		logger.Info("karpenter CRD absent; skipping reader")
	}
	readers.AttachDynamic(vpa, karpenter)

	healthSrv := startHealthServer(*healthAddr, logger, readers)

	logger.Info("agent ready",
		"cluster_id", *clusterID,
		"vpa", vpa != nil,
		"karpenter", karpenter != nil,
	)

	<-ctx.Done()
	logger.Info("agent stopping")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("health server shutdown", "err", err)
	}
	logger.Info("agent stopped")
	return 0
}

// loadK8sConfig prefers in-cluster config (the production path inside
// a pod with a projected service-account token). Out-of-cluster dev
// uses --kubeconfig or $KUBECONFIG.
func loadK8sConfig(kubeconfig string) (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}
	if kubeconfig == "" {
		return nil, errors.New("no in-cluster config and no --kubeconfig provided")
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

// startHealthServer returns 200 on /healthz once readers are non-nil.
// Kubelet probes hit this; mTLS egress + SaaS reachability are
// additional concerns layered in Phase 5 PR #2.
func startHealthServer(addr string, log *slog.Logger, readers *cluster.Readers) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if readers == nil || readers.Events == nil {
			http.Error(w, "informers not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("health server", "err", err)
		}
	}()
	return srv
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
