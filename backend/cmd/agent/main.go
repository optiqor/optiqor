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
	"github.com/optiqor/optiqor/internal/agent/egress"
	"github.com/optiqor/optiqor/internal/agent/prom"
	"github.com/optiqor/optiqor/internal/platform/telemetry"
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
	tenantID := flag.String("tenant-id", os.Getenv("OPTIQOR_TENANT_ID"), "owning tenant id; required for egress")
	clusterID := flag.String("cluster-id", os.Getenv("OPTIQOR_CLUSTER_ID"), "stable cluster identifier; required")
	healthAddr := flag.String("health-addr", envOr("OPTIQOR_HEALTH_ADDR", ":8088"), "address for /healthz (kubelet probes)")
	metricsAddr := flag.String("metrics-addr", envOr("OPTIQOR_METRICS_ADDR", ":9090"), "address for /metrics (customer Prometheus scrapes the agent itself)")
	resyncStr := flag.String("resync", envOr("OPTIQOR_INFORMER_RESYNC", "10m"), "informer cache resync period")
	snapshotIntervalStr := flag.String("snapshot-interval", envOr("OPTIQOR_SNAPSHOT_INTERVAL", "60s"), "how often to POST a snapshot to the backend")
	ingestURL := flag.String("ingest-url", envOr("OPTIQOR_INGEST_URL", ""), "https URL the snapshots POST to; empty disables egress")
	promURL := flag.String("prometheus-url", envOr("OPTIQOR_PROMETHEUS_URL", ""), "Prometheus /api/v1/query base URL; empty disables scraping")
	promAuthMode := flag.String("prometheus-auth", envOr("OPTIQOR_PROMETHEUS_AUTH", "none"), "none | bearer | basic | sa_token | mtls")
	promBearerFile := flag.String("prometheus-bearer-file", envOr("OPTIQOR_PROMETHEUS_BEARER_FILE", ""), "path to a bearer token file (auto-refreshed)")
	promUser := flag.String("prometheus-user", envOr("OPTIQOR_PROMETHEUS_USER", ""), "basic-auth username")
	promCertFile := flag.String("prometheus-cert-file", envOr("OPTIQOR_PROMETHEUS_CERT_FILE", ""), "mTLS client cert (PEM)")
	promKeyFile := flag.String("prometheus-key-file", envOr("OPTIQOR_PROMETHEUS_KEY_FILE", ""), "mTLS client key (PEM)")
	promCAFile := flag.String("prometheus-ca-file", envOr("OPTIQOR_PROMETHEUS_CA_FILE", ""), "optional CA bundle for mTLS / private CA")
	promInstantTTLStr := flag.String("prometheus-instant-ttl", envOr("OPTIQOR_PROMETHEUS_INSTANT_TTL", "60s"), "TTL cache for instant queries")
	promRangeTTLStr := flag.String("prometheus-range-ttl", envOr("OPTIQOR_PROMETHEUS_RANGE_TTL", "24h"), "TTL cache for range queries; 30d roll-ups run once daily")
	eventsCutoffStr := flag.String("events-cutoff", envOr("OPTIQOR_EVENTS_CUTOFF", "10m"), "drop events older than this from the snapshot")
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

	metricsReg := telemetry.NewRegistry()
	healthSrv := startHealthServer(*healthAddr, logger, readers)
	metricsSrv := startMetricsServer(*metricsAddr, logger, metricsReg)

	snapshotInterval, err := time.ParseDuration(*snapshotIntervalStr)
	if err != nil {
		logger.Error("bad --snapshot-interval", "value", *snapshotIntervalStr, "err", err)
		return 2
	}
	var egressClient *egress.Client
	if *ingestURL != "" {
		secret := []byte(os.Getenv("OPTIQOR_INGEST_SECRET"))
		if len(secret) < 32 {
			logger.Error("OPTIQOR_INGEST_SECRET must be >= 32 bytes when --ingest-url is set")
			return 2
		}
		if *tenantID == "" {
			logger.Error("--tenant-id is required when --ingest-url is set")
			return 2
		}
		egressClient, err = egress.New(egress.Config{
			BackendURL:   *ingestURL,
			TenantID:     *tenantID,
			ClusterID:    *clusterID,
			AgentVersion: version,
			IngestSecret: secret,
		})
		if err != nil {
			logger.Error("egress init failed", "err", err)
			return 1
		}
		eventsCutoff, perr := time.ParseDuration(*eventsCutoffStr)
		if perr != nil {
			logger.Error("bad --events-cutoff", "value", *eventsCutoffStr, "err", perr)
			return 2
		}
		var scraper *prom.Scraper
		if *promURL != "" {
			authCfg := prom.AuthConfig{
				Mode:           prom.AuthMode(*promAuthMode),
				BearerToken:    os.Getenv("OPTIQOR_PROMETHEUS_BEARER_TOKEN"),
				BearerFile:     *promBearerFile,
				Username:       *promUser,
				Password:       os.Getenv("OPTIQOR_PROMETHEUS_PASSWORD"),
				ClientCertFile: *promCertFile,
				ClientKeyFile:  *promKeyFile,
				CABundleFile:   *promCAFile,
			}
			obs := prom.NewTelemetryObserver(metricsReg)
			var pc *prom.HTTPClient
			if authCfg.Mode == prom.AuthMTLS {
				pc, perr = prom.NewHTTPClientWithMTLS(*promURL, authCfg, prom.WithObserver(obs))
			} else {
				auth, aerr := prom.NewAuthenticator(authCfg)
				if aerr != nil {
					logger.Error("prom auth init failed", "mode", authCfg.Mode, "err", aerr)
					return 2
				}
				pc, perr = prom.NewHTTPClient(*promURL, prom.WithAuth(auth), prom.WithObserver(obs))
			}
			if perr != nil {
				logger.Error("prom client init failed", "url", *promURL, "err", perr)
				return 1
			}
			instantTTL, terr := time.ParseDuration(*promInstantTTLStr)
			if terr != nil {
				logger.Error("bad --prometheus-instant-ttl", "value", *promInstantTTLStr, "err", terr)
				return 2
			}
			rangeTTL, terr := time.ParseDuration(*promRangeTTLStr)
			if terr != nil {
				logger.Error("bad --prometheus-range-ttl", "value", *promRangeTTLStr, "err", terr)
				return 2
			}
			cached := prom.NewCachingClient(pc, instantTTL, rangeTTL)
			scraper = prom.NewScraper(cached, nil)
		} else {
			logger.Info("prometheus-url empty; scrape loop disabled (verify.sh GAP remains)")
		}
		pop := &populator{
			readers:      readers,
			scraper:      scraper,
			clusterID:    *clusterID,
			agentVersion: version,
			probe:        newK8sProbe(cs.Discovery(), dynCli),
			eventsCutoff: eventsCutoff,
		}
		go runSnapshotLoop(ctx, logger, egressClient, pop, snapshotInterval)
	} else {
		logger.Info("ingest-url empty; running in observe-only mode")
	}

	logger.Info("agent ready",
		"cluster_id", *clusterID,
		"vpa", vpa != nil,
		"karpenter", karpenter != nil,
		"egress", egressClient != nil,
	)

	<-ctx.Done()
	logger.Info("agent stopping")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("health server shutdown", "err", err)
	}
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("metrics server shutdown", "err", err)
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
// Kubelet probes hit this; the address stays distinct from /metrics so
// RBAC + NetworkPolicy can scope the two independently (per ADR-0021).
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

// startMetricsServer serves /metrics on a dedicated port. Customer
// Prometheus scrapes this to watch the agent itself; the agent does
// NOT scrape customer workloads through this endpoint (ADR-0021).
func startMetricsServer(addr string, log *slog.Logger, reg *telemetry.Registry) *http.Server {
	mux := http.NewServeMux()
	if reg != nil {
		mux.Handle("GET /metrics", reg.Handler())
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics server", "err", err)
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

// runSnapshotLoop fires every interval until ctx is cancelled. Builds
// the rich payload via populator, refreshes the Prom scraper's
// pod→workload index every tick (informer caches drift), then POSTs.
// Never panics; one bad post logs and waits for the next tick so a
// transient backend blip doesn't kill the loop.
func runSnapshotLoop(ctx context.Context, log *slog.Logger, client *egress.Client, pop *populator, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	batch := 0
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			batch++
			if pop.scraper != nil && pop.readers != nil && pop.readers.Workloads != nil {
				owners := pop.readers.Workloads.PodOwners(ctx)
				pop.scraper.SetPodOwners(remapOwnerKeys(owners))
			}
			batchID := fmt.Sprintf("%s-%d", pop.clusterID, batch)
			snap, perr := pop.Build(ctx, batchID, now.UTC(), interval)
			if perr != nil {
				log.WarnContext(ctx, "snapshot partial", "batch", batchID, "err", perr)
			}
			postCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			resp, err := client.PostSnapshot(postCtx, snap)
			cancel()
			if err != nil {
				log.WarnContext(ctx, "snapshot post failed", "batch", snap.BatchID, "err", err)
				continue
			}
			log.InfoContext(ctx, "snapshot accepted",
				"batch", resp.BatchID,
				"workloads", resp.WorkloadsObs,
				"events", resp.EventsObs,
				"hpas", resp.HPAsObs,
				"prom_samples", resp.PromObs,
				"provisioner_class", snap.ProvisionerClass,
			)
		}
	}
}

// remapOwnerKeys converts cluster.WorkloadIndex types to the prom
// scraper's parallel types. The duplication keeps the prom package
// from importing cluster (which would pull in client-go on the SaaS
// side); the bridge happens once per tick in the cmd binary.
func remapOwnerKeys(in map[cluster.PodOwnerKey]cluster.PodWorkloadKey) map[prom.PodKey]prom.WorkloadKey {
	if len(in) == 0 {
		return nil
	}
	out := make(map[prom.PodKey]prom.WorkloadKey, len(in))
	for k, v := range in {
		out[prom.PodKey{Namespace: k.Namespace, Pod: k.Pod}] = prom.WorkloadKey{
			Namespace: v.Namespace,
			Kind:      v.Kind,
			Name:      v.Name,
		}
	}
	return out
}
