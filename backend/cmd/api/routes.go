// Domain-handler wiring for the api binary.
//
// Every Phase-1+ HTTP route the customer sees lives in a dedicated
// internal/<domain> package; this file is the single place where the
// boot path composes their handlers onto the mux. Keep handler
// construction here lean so cmd/api/main.go can stay focused on
// process-lifecycle concerns (config, signals, shutdown).
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/agent/ident"
	"github.com/optiqor/optiqor/internal/agent/llm/anthropic"
	"github.com/optiqor/optiqor/internal/applyfix/attribution"
	"github.com/optiqor/optiqor/internal/auth"
	"github.com/optiqor/optiqor/internal/billing"
	"github.com/optiqor/optiqor/internal/cost"
	"github.com/optiqor/optiqor/internal/dashboard"
	"github.com/optiqor/optiqor/internal/ingestion"
	"github.com/optiqor/optiqor/internal/onboarding"
	"github.com/optiqor/optiqor/internal/onboarding/preflight"
	"github.com/optiqor/optiqor/internal/platform/config"
	"github.com/optiqor/optiqor/internal/platform/ratelimit"
	"github.com/optiqor/optiqor/internal/prwriter"
	"github.com/optiqor/optiqor/internal/receipts"
	"github.com/optiqor/optiqor/internal/sandbox"
	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/vcs"
	"github.com/optiqor/optiqor/internal/worker/workflows"
)

// 60/min/IP covers a developer iterating in a browser while pinning the
// $0.40/PR LLM cap and the 3s p95 budget against a runaway script.
// Redis-backed limiter swaps in Phase 5 with the same Middleware shape.
const (
	sandboxRateLimit  = 60
	sandboxRateWindow = time.Minute
)

// domainDeps is the set of constructed handlers + stores threaded
// through to main(). One owner per surface; cmd/api/main.go reaches
// into this struct rather than building handlers itself.
type domainDeps struct {
	Sandbox       *sandbox.Handler
	Receipts      *receipts.Handler
	PRWriter      *prwriter.Handler
	Ingest        *ingestion.Handler
	AgentSnapshot *ingestion.AgentSnapshotHandler
	Spike         *billing.SpikeHandler
	Auth          *auth.Handler
	Onboarding    *onboarding.Handler
	Preflight     *preflight.Handler
	Dashboard     *dashboard.Handler
	Attribution   *attribution.Handler // nil until Postgres is wired
}

// noopLLM is the dev-mode LLMClient — returns a deterministic stub so
// the dashboard renders without an Anthropic API key. Production
// swaps via pickLLM when OPTIQOR_ANTHROPIC_API_KEY is set.
type noopLLM struct{}

func (noopLLM) Generate(_ context.Context, _ agent.LLMRequest) (agent.LLMResponse, error) {
	return agent.LLMResponse{
		Text:  "EXPLANATION:\nLLM not configured in this environment.\nDIFF:\n",
		Model: "noop",
	}, nil
}

// pickLLM selects the production Anthropic adapter when an API key is
// configured. An init failure logs and falls back to noopLLM so a bad
// key never takes the api binary down — Apply Fix returns an empty
// diff until the operator corrects the key.
func pickLLM(cfg config.Config, log *slog.Logger) agent.LLMClient {
	if cfg.AnthropicAPIKey == "" {
		return noopLLM{}
	}
	client, err := anthropic.New(anthropic.Config{APIKey: cfg.AnthropicAPIKey})
	if err != nil {
		log.Error("anthropic init failed; using noop LLM", "err", err)
		return noopLLM{}
	}
	return client
}

// pickRecorder returns the production PgRecorder when Postgres is
// connected; otherwise nil so the Composer skips attribution writes.
// llm_calls is RLS-bound so a nil-pool boot cannot accidentally leak.
func pickRecorder(pool *pgxpool.Pool) agent.BudgetRecorder {
	if pool == nil {
		return nil
	}
	return agent.NewPgRecorder(pool)
}

// noopSpikeDispatcher logs the spike at debug level and acks. The
// real implementation routes through the worker dispatcher into the
// CostSpike workflow.
type noopSpikeDispatcher struct{}

func (noopSpikeDispatcher) DispatchSpike(_ tenancy.Context, _ billing.SpikeEnvelope) error {
	return nil
}

// buildDomainDeps wires together the Phase-1 default implementations
// of every domain handler. Production cmd/api swaps these for the
// real adapters (real LLMClient, real Stores, real Temporal-backed
// SpikeDispatcher); the wiring shape stays the same.
func buildDomainDeps(cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) *domainDeps {
	pricer := cost.NewStaticPricer()
	region := "us-east-1"

	sandboxH := &sandbox.Handler{
		Store:         sandbox.NewInMemoryStore(),
		Pricer:        pricer,
		Region:        region,
		Now:           func() time.Time { return time.Now().UTC() },
		PublicBaseURL: os.Getenv("OPTIQOR_PUBLIC_URL"), // "" → derive from request
	}

	receiptsStore := receipts.NewInMemoryStore()
	receiptsReg := receipts.NewStaticRegistry()
	receiptsH := &receipts.Handler{Store: receiptsStore, Registry: receiptsReg}

	composer := &agent.Composer{
		LLM:      pickLLM(cfg, log),
		Budget:   agent.Budget{PerCallCents: 40}, // $0.40 cap per backend CLAUDE.md
		Recorder: pickRecorder(pool),
	}
	prH := &prwriter.Handler{Composer: composer}

	ingestH := &ingestion.Handler{}

	spikeH := &billing.SpikeHandler{Dispatcher: noopSpikeDispatcher{}}

	authH := &auth.Handler{Signer: buildSessionSigner()}
	onboardingH := &onboarding.Handler{Service: onboarding.NewService(onboarding.NewInMemoryStore())}
	// Pre-flight runs with a noop probe in dev — the wizard's
	// preview page only lights up when the operator-side probe lands
	// (Phase 5 close-out follow-on). Routing the endpoint now so the
	// web side can call it without a 404 in dev.
	preflightH := &preflight.Handler{Runner: nil}

	return &domainDeps{
		Sandbox:       sandboxH,
		Receipts:      receiptsH,
		PRWriter:      prH,
		Ingest:        ingestH,
		AgentSnapshot: buildAgentSnapshotHandler(log, pool),
		Spike:         spikeH,
		Auth:          authH,
		Onboarding:    onboardingH,
		Preflight:     preflightH,
		Dashboard:     buildDashboardHandler(pool),
		Attribution:   buildAttributionHandler(pool),
	}
}

// buildAgentSnapshotHandler returns the verifier-backed handler when
// OPTIQOR_AGENT_INGEST_SECRET is configured. Dev mode without the
// secret returns nil and the mux skips the route — agents in dev
// cannot exercise the path, which is correct (they wouldn't have a
// secret to issue tokens with either). Production wires a PgSink so
// snapshots refresh the agents table + clusters.node_provisioner_class
// rather than evaporating at the 202 boundary.
func buildAgentSnapshotHandler(log *slog.Logger, pool *pgxpool.Pool) *ingestion.AgentSnapshotHandler {
	secret := os.Getenv("OPTIQOR_AGENT_INGEST_SECRET")
	if secret == "" {
		log.Warn("OPTIQOR_AGENT_INGEST_SECRET unset; agent snapshot endpoint disabled")
		return nil
	}
	ver, err := ident.NewVerifier([]byte(secret))
	if err != nil {
		log.Error("agent ident verifier init failed", "err", err)
		return nil
	}
	var sink ingestion.AgentSnapshotSink
	if pool != nil {
		sink = ingestion.NewAgentSnapshotPgSink(pool)
	} else {
		log.Warn("no Postgres pool; agent snapshot persistence disabled (dashboard agent-health pill will report offline)")
	}
	return &ingestion.AgentSnapshotHandler{Verifier: ver, Sink: sink}
}

// buildDashboardHandler wires AgentHealthPgStore when Postgres is
// available. SavingsSource + ApplyFixesSource stay nil — both already
// fall back to the demo-data shape inside the handler, which is the
// right UX for Phase 5 (real merges land in Phase 6 alongside CUR).
func buildDashboardHandler(pool *pgxpool.Pool) *dashboard.Handler {
	h := &dashboard.Handler{Now: func() time.Time { return time.Now().UTC() }}
	if pool != nil {
		h.Agent = dashboard.NewAgentHealthPgStore(pool)
	}
	return h
}

// buildAttributionHandler returns the merged-PR cost-attribution
// orchestrator when Postgres is wired. Dev mode (nil pool) returns
// nil; cmd/api/main.go's webhook router treats that as "skip dispatch".
func buildAttributionHandler(pool *pgxpool.Pool) *attribution.Handler {
	if pool == nil {
		return nil
	}
	return &attribution.Handler{
		Resolver: attribution.NewPgResolver(pool),
		Store:    attribution.NewPgStore(pool),
		Poster:   attribution.NewVCSPoster(vcs.NewGitHub()),
	}
}

// buildSessionSigner reads OPTIQOR_SESSION_SECRET, falling back to a
// dev-only secret so the dashboard runs without a config step. Phase-5
// flips it to config.Validate-enforced so prod boots fail closed.
func buildSessionSigner() *auth.Signer {
	secret := os.Getenv("OPTIQOR_SESSION_SECRET")
	if secret == "" {
		// The "-dev" suffix surfaces in token inspection so a leaked
		// dev token is unambiguous.
		secret = "00000000000000000000000000000-dev"
	}
	return auth.NewSigner([]byte(secret))
}

// mountDomainRoutes wires every domain route on mux. Sandbox is
// IP-rate-limited; apply-fixes + onboarding go through the tenant
// extractor; everything else is unauth public.
func mountDomainRoutes(mux *http.ServeMux, deps *domainDeps, env config.Env) {
	// Bypass Handler.Mount so the limiter gets in front of the unauth
	// sandbox routes. FailOpen so a limiter blip can't 503 the sandbox.
	sandboxMW := ratelimit.Middleware(ratelimit.Options{
		Limiter:  ratelimit.NewMemory(sandboxRateLimit, sandboxRateWindow),
		FailOpen: true,
	})
	mux.Handle("POST /v1/analyze", sandboxMW(http.HandlerFunc(deps.Sandbox.Analyze)))
	mux.Handle("GET /r/{hash}", sandboxMW(http.HandlerFunc(deps.Sandbox.Share)))

	deps.Receipts.Mount(mux)
	deps.Spike.Mount(mux)
	deps.Auth.Mount(mux)

	tenantMW := func(next http.Handler) http.Handler {
		return requireTenant(HeaderTenantExtractor, next)
	}
	// agentMW prefers the mTLS-derived SPIFFE id. Header fallback is
	// dev/staging only — production cannot accept a tenant id from a
	// caller-controlled header on the agent ingest path. Production
	// TLS terminates the cert chain upstream of the api binary's mux.
	agentExtractor := MTLSTenantExtractor(env)
	agentMW := func(next http.Handler) http.Handler {
		return requireTenant(agentExtractor, next)
	}
	mux.Handle("POST /v1/ingest", tenantMW(http.HandlerFunc(deps.Ingest.Ingest)))
	if deps.AgentSnapshot != nil {
		mux.Handle("POST /v1/agent/snapshot", agentMW(http.HandlerFunc(deps.AgentSnapshot.Snapshot)))
	}
	if deps.Dashboard != nil {
		mux.Handle("GET /v1/savings/summary", tenantMW(http.HandlerFunc(deps.Dashboard.Summary)))
		mux.Handle("GET /v1/apply-fixes", tenantMW(http.HandlerFunc(deps.Dashboard.ListApplyFixes)))
		mux.Handle("GET /v1/agent/health", tenantMW(http.HandlerFunc(deps.Dashboard.AgentHealth)))
	}
	mux.Handle("POST /v1/apply-fixes", tenantMW(http.HandlerFunc(deps.PRWriter.Preview)))
	mux.Handle("GET /v1/onboarding/state", tenantMW(http.HandlerFunc(deps.Onboarding.GetState)))
	mux.Handle("POST /v1/onboarding/transition", tenantMW(http.HandlerFunc(deps.Onboarding.Transition)))
	mux.Handle("GET /v1/onboarding/health", tenantMW(http.HandlerFunc(deps.Onboarding.GetHealth)))
	if deps.Preflight != nil {
		mux.Handle("POST /v1/onboarding/preflight", tenantMW(http.HandlerFunc(deps.Preflight.Run)))
	}
}

// Compile-time ensure the workflows package is wired so its
// registrations stay in sync with the routes that submit jobs into
// them.
var _ = workflows.ApplyFix{}

// metaHandler serves a tiny JSON manifest at GET /v1/meta that
// summarises the API surface for the dashboard. Wired so the
// frontend can discover endpoints without reading our docs.
func metaHandler(w http.ResponseWriter, _ *http.Request) {
	type endpoint struct {
		Method string `json:"method"`
		Path   string `json:"path"`
		Notes  string `json:"notes,omitempty"`
	}
	type meta struct {
		Version   string     `json:"version"`
		Endpoints []endpoint `json:"endpoints"`
	}
	out := meta{
		Version: version,
		Endpoints: []endpoint{
			{Method: "POST", Path: "/v1/analyze", Notes: "sandbox: returns findings + savings"},
			{Method: "GET", Path: "/r/{hash}", Notes: "sandbox: fetch shared analysis"},
			{Method: "GET", Path: "/v1/receipts/{id}", Notes: "verify a signed Receipt"},
			{Method: "POST", Path: "/v1/apply-fixes", Notes: "preview the Apply Fix PR body + diff"},
			{Method: "POST", Path: "/v1/ingest", Notes: "agent → SaaS metrics ingestion"},
			{Method: "POST", Path: "/v1/cost-spikes", Notes: "bill anomaly webhook"},
			{Method: "GET", Path: "/v1/session/whoami", Notes: "dashboard: identity + tenant context"},
			{Method: "POST", Path: "/v1/session/issue", Notes: "Auth.js bridge: mint a backend JWT"},
			{Method: "GET", Path: "/v1/onboarding/state", Notes: "dashboard: tenant onboarding progress"},
			{Method: "POST", Path: "/v1/onboarding/transition", Notes: "dashboard: advance onboarding stage"},
			{Method: "GET", Path: "/v1/onboarding/health", Notes: "dashboard: funnel position + blockers"},
			{Method: "POST", Path: "/v1/onboarding/preflight", Notes: "install wizard: cluster pre-flight checks"},
			{Method: "GET", Path: "/v1/savings/summary", Notes: "dashboard: lifetime / MTD / YTD savings"},
			{Method: "GET", Path: "/v1/apply-fixes", Notes: "dashboard: list apply fixes filtered by state"},
			{Method: "GET", Path: "/v1/agent/health", Notes: "dashboard: agent status + last check-in"},
			{Method: "GET", Path: "/healthz", Notes: "liveness"},
			{Method: "GET", Path: "/readyz", Notes: "readiness"},
			{Method: "GET", Path: "/metrics", Notes: "prometheus metrics"},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
