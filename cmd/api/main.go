// Command api is the Optiqor HTTP API server.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/optiqor/optiqor/internal/applyfix/attribution"
	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/platform/healthz"
	"github.com/optiqor/optiqor/internal/platform/httperr"
	"github.com/optiqor/optiqor/internal/platform/logging"
	"github.com/optiqor/optiqor/internal/platform/telemetry"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/vcs"
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

	checks := healthz.NewRegistry()
	checks.Register("self", healthz.AlwaysOK)

	metrics := telemetry.NewRegistry()
	httpRequests := metrics.NewCounter("optiqor_http_requests_total",
		"Total HTTP requests served by the api binary, by route and status",
		nil)
	httpLatency := metrics.NewHistogram("optiqor_http_request_duration_seconds",
		"HTTP request latency in seconds",
		nil,
		[]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5})

	pool, err := openPool(cfg, logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pool:", err)
		return 2
	}
	if pool != nil {
		defer pool.Close()
	}

	deps := buildDomainDeps(cfg, pool, logger)
	mux := buildMux(checks, logger, []byte(cfg.GitHubAppWebhookSecret), metrics, deps)
	mountDomainRoutes(mux, deps)
	mux.HandleFunc("GET /v1/meta", metaHandler)
	handler := http.Handler(mux)
	handler = withAccessLog(logger, httpRequests, httpLatency, handler)
	handler = withPanicRecovery(logger, handler)
	handler = withRequestID(handler)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	listenErr := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", cfg.HTTPAddr, "version", version, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
			cancel()
			return
		}
		listenErr <- nil
	}()

	select {
	case <-ctx.Done():
	case err := <-listenErr:
		if err != nil {
			logger.Error("listen failed", "err", err)
			return 1
		}
	}

	logger.Info("api shutting down", "grace", cfg.ShutdownGrace)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "err", err)
		return 1
	}
	logger.Info("api stopped")
	return 0
}

// oauthStateRE constrains the state token to the Auth.js / RFC 6749
// alphabet so a tainted state cannot smuggle path or query bytes into
// the redirect destination. Anything outside the set is dropped, not
// rejected — the dashboard can still complete login without state.
var oauthStateRE = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]{1,256}$`)

// oauthRedirectTarget resolves where to send the browser after a
// GitHub OAuth callback. The base origin comes ONLY from
// OPTIQOR_DASHBOARD_URL (or the hardcoded localhost dev default) —
// r.Host is never trusted, since Host can be spoofed and would let an
// attacker craft a callback that bounces the browser to their domain
// (gosec G710 open redirect).
func oauthRedirectTarget(state string) string {
	base := os.Getenv("OPTIQOR_DASHBOARD_URL")
	if base == "" {
		base = "http://localhost:3000"
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		// Misconfigured env value — fall back to the dev default rather
		// than echo the broken URL back to the browser.
		u, _ = url.Parse("http://localhost:3000")
	}
	u.Path = "/app"
	q := url.Values{"oauth": []string{"github"}}
	if oauthStateRE.MatchString(state) {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// dispatchGitHubEvent routes a verified webhook to its domain handler.
// Failure modes are logged but never propagated to the caller — the
// 202 ack must fire so GitHub does not retry on transient downstream
// errors. Production scheduling (the retry queue) lives in the worker.
func dispatchGitHubEvent(ctx context.Context, log *slog.Logger, deps *domainDeps, event, delivery string, body []byte) {
	if event != "pull_request" {
		return
	}
	if deps == nil || deps.Attribution == nil {
		log.WarnContext(ctx, "attribution not configured; skipping",
			"event", event, "delivery", delivery)
		return
	}
	ev, err := attribution.ParseGitHub(body)
	if err != nil {
		if errors.Is(err, attribution.ErrNotMerged) {
			return
		}
		log.WarnContext(ctx, "github webhook parse failed",
			"event", event, "delivery", delivery, "err", err)
		return
	}
	if err := deps.Attribution.OnMerged(ctx, ev); err != nil {
		// ErrUnknownInstallation / ErrNoMatchingApplyFix are expected
		// non-customer PRs — log at info to avoid alert noise.
		level := slog.LevelError
		if errors.Is(err, attribution.ErrUnknownInstallation) || errors.Is(err, attribution.ErrNoMatchingApplyFix) {
			level = slog.LevelInfo
		}
		log.Log(ctx, level, "attribution.OnMerged",
			"delivery", delivery, "repo", ev.RepoOwner+"/"+ev.RepoName,
			"pr", ev.PRNumber, "err", err)
	}
}

// openPool returns a pgxpool when cfg.PostgresDSN is set. Dev mode
// without a DSN returns (nil, nil) so the in-memory paths still boot;
// config.Validate guarantees prod has a DSN. The pool runs as
// optiqor_app — every RLS-bound writer binds app.tenant_id per call.
func openPool(cfg config.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	if cfg.PostgresDSN == "" {
		log.Warn("no OPTIQOR_POSTGRES_DSN — RLS-bound stores will be disabled")
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

// buildMux returns the HTTP routes the api serves. Exposed so tests can
// hit handlers without spinning a real socket. Empty webhookSecret =
// dev mode (config.Validate enforces it in prod); nil metrics skips the
// /metrics endpoint. deps may be nil for the minimal /healthz tests.
func buildMux(checks *healthz.Registry, logger *slog.Logger, webhookSecret []byte, metrics *telemetry.Registry, deps *domainDeps) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		results, ok := checks.Run(r.Context(), 2*time.Second)
		status := http.StatusOK
		if !ok {
			status = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      ok,
			"checks":  results,
			"version": version,
		})
	})

	if metrics != nil {
		mux.Handle("GET /metrics", metrics.Handler())
	}

	// Phase 5 wires the code-exchange + session-issuance flow; Phase 1
	// redirects to the dashboard so the user lands somewhere useful
	// instead of staring at a JSON blob in their browser.
	mux.HandleFunc("GET /oauth/github/callback", func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")
		if code == "" {
			httperr.MissingField(w, r, "code")
			return
		}
		// Never log the raw code; only that one was received and the
		// state token (binds the redirect to the originator).
		logger.InfoContext(r.Context(), "github oauth callback",
			"state_len", len(state),
			"code_len", len(code),
		)
		dest := oauthRedirectTarget(state)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, dest, http.StatusSeeOther) //nolint:gosec // G710: dest origin is OPTIQOR_DASHBOARD_URL or localhost, state passes oauthStateRE; r.Host is never trusted.
	})

	// pprof gated on OPTIQOR_ADMIN_TOKEN header. Empty token disables
	// pprof entirely (safe default in dev).
	if os.Getenv("OPTIQOR_ADMIN_TOKEN") != "" {
		mountPProf(mux, os.Getenv("OPTIQOR_ADMIN_TOKEN"))
	}

	gh := vcs.NewGitHub()
	mux.HandleFunc("POST /webhooks/github", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, config.GitHubWebhookMaxBytes))
		if err != nil {
			if httperr.IsBodyTooLarge(err) {
				httperr.BodyTooLarge(w, r, config.GitHubWebhookMaxBytes)
				return
			}
			httperr.BadRequest(w, r, "could not read webhook body")
			return
		}
		signature := r.Header.Get("X-Hub-Signature-256")
		if len(webhookSecret) == 0 {
			logger.WarnContext(r.Context(), "github webhook signature not verified (dev mode)")
		} else if err := gh.VerifyWebhook(webhookSecret, signature, body); err != nil {
			logger.WarnContext(r.Context(), "github webhook rejected", "err", err)
			httperr.Unauthorized(w, r, "invalid webhook signature")
			return
		}
		event := r.Header.Get("X-GitHub-Event")
		delivery := r.Header.Get("X-GitHub-Delivery")
		logger.InfoContext(r.Context(), "github webhook accepted",
			"event", event, "delivery", delivery, "bytes", len(body))

		dispatchGitHubEvent(r.Context(), logger, deps, event, delivery, body)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":              "accepted",
			"event":               event,
			"delivery":            delivery,
			"retry_after_seconds": 5,
		})
	})

	return mux
}

// TenantExtractor is pluggable so the Phase-5 JWT extractor can replace
// the Phase-1 header reader without touching middleware call sites.
type TenantExtractor func(r *http.Request) (tenancy.Context, error)

// HeaderTenantExtractor reads X-Optiqor-Tenant. Public endpoints
// (/healthz, /readyz, /webhooks/*) must route AROUND this middleware.
func HeaderTenantExtractor(r *http.Request) (tenancy.Context, error) {
	id := r.Header.Get("X-Optiqor-Tenant")
	if id == "" {
		return tenancy.Context{}, tenancy.ErrNoTenant
	}
	return tenancy.Context{
		TenantID:    id,
		WorkspaceID: r.Header.Get("X-Optiqor-Workspace"),
		ClusterID:   r.Header.Get("X-Optiqor-Cluster"),
		Namespace:   r.Header.Get("X-Optiqor-Namespace"),
	}, nil
}

// requireTenant guarantees a validated tenant scope in the context of
// every request reaching next.
func requireTenant(extract TenantExtractor, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t, err := extract(r)
		if err != nil {
			httperr.Unauthorized(w, r, "tenant required — set X-Optiqor-Tenant header or attach a session JWT")
			return
		}
		if err := t.Validate(); err != nil {
			httperr.WriteWithDetails(w, r, http.StatusUnauthorized, httperr.CodeUnauthorized,
				"invalid tenant context: "+err.Error(),
				map[string]any{"hint": "tenant id must be a non-empty UUID"})
			return
		}
		ctx := tenancy.WithContext(r.Context(), t)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// withRequestID assigns or echoes X-Request-ID and stashes it on the
// context so logs/traces can join on it.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := logging.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Never block requests on entropy starvation.
		return fmt.Sprintf("t-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// withPanicRecovery turns a panicking handler into a 500 and keeps the
// surrounding server alive. Middleware order is request-id →
// access-log → panic-recovery so panics still carry a request id and
// the access log records the 500.
func withPanicRecovery(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			logger.ErrorContext(r.Context(), "panic recovered",
				"err", fmt.Sprintf("%v", rec),
				"path", r.URL.Path,
				"method", r.Method,
				"stack", string(debug.Stack()),
			)
			// If the handler already committed the response this is a
			// no-op + stdlib warning; we still want the log line. Use the
			// structured envelope so panics surface with the same shape
			// integrators see for any other 5xx.
			httperr.Internal(w, r, "")
		}()
		next.ServeHTTP(w, r)
	})
}

// withAccessLog emits one log line + Prometheus sample per request.
// Sub-200ms healthy requests drop to debug to keep info-level noise
// down when the api is steady.
func withAccessLog(logger *slog.Logger, requests telemetry.Counter, latency telemetry.Histogram, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recordingWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		dur := time.Since(start)

		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400:
			level = slog.LevelWarn
		case dur < 200*time.Millisecond:
			level = slog.LevelDebug
		}
		logger.Log(r.Context(), level, "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", dur.Milliseconds(),
		)

		if requests != nil {
			requests.Inc()
		}
		if latency != nil {
			latency.Observe(dur.Seconds())
		}
	})
}

// mountPProf wires the stdlib pprof handlers behind a constant-time
// header check. Operators fetch profiles via:
//
//	curl -H "X-Admin-Token: $TOKEN" https://api.optiqor.dev/debug/pprof/heap > heap.pb
func mountPProf(mux *http.ServeMux, token string) {
	require := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("X-Admin-Token")
			// Constant-time compare blocks the timing side-channel.
			if subtleConstantTimeEq(got, token) != 1 {
				httperr.Unauthorized(w, r, "X-Admin-Token header missing or invalid")
				return
			}
			h.ServeHTTP(w, r)
		})
	}
	mux.Handle("GET /debug/pprof/", require(http.HandlerFunc(pprofIndex)))
	mux.Handle("GET /debug/pprof/cmdline", require(http.HandlerFunc(pprofCmdline)))
	mux.Handle("GET /debug/pprof/profile", require(http.HandlerFunc(pprofProfile)))
	mux.Handle("GET /debug/pprof/symbol", require(http.HandlerFunc(pprofSymbol)))
	mux.Handle("GET /debug/pprof/trace", require(http.HandlerFunc(pprofTrace)))
}

// recordingWriter intercepts status + byte count without changing the
// streaming behaviour of the underlying ResponseWriter.
type recordingWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *recordingWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *recordingWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}
