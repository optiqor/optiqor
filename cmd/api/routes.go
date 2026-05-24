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
	"net/http"
	"os"
	"time"

	"github.com/optiqor/optiqor/internal/agent"
	"github.com/optiqor/optiqor/internal/auth"
	"github.com/optiqor/optiqor/internal/billing"
	"github.com/optiqor/optiqor/internal/cost"
	"github.com/optiqor/optiqor/internal/ingestion"
	"github.com/optiqor/optiqor/internal/onboarding"
	"github.com/optiqor/optiqor/internal/platform/ratelimit"
	"github.com/optiqor/optiqor/internal/prwriter"
	"github.com/optiqor/optiqor/internal/receipts"
	"github.com/optiqor/optiqor/internal/sandbox"
	"github.com/optiqor/optiqor/internal/tenancy"
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
	Sandbox    *sandbox.Handler
	Receipts   *receipts.Handler
	PRWriter   *prwriter.Handler
	Ingest     *ingestion.Handler
	Spike      *billing.SpikeHandler
	Auth       *auth.Handler
	Onboarding *onboarding.Handler
}

// noopLLM is a Phase-1 default LLMClient. It returns an empty diff
// and a fixed explanation. cmd/api wires the real Anthropic client
// behind the same interface when the API key is configured.
type noopLLM struct{}

func (noopLLM) Generate(_ context.Context, _ agent.LLMRequest) (agent.LLMResponse, error) {
	return agent.LLMResponse{
		Text:  "EXPLANATION:\nLLM not configured in this environment.\nDIFF:\n",
		Model: "noop",
	}, nil
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
func buildDomainDeps() *domainDeps {
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
		LLM:      noopLLM{},
		Budget:   agent.Budget{PerCallCents: 40}, // $0.40 cap per backend CLAUDE.md
		Recorder: nil,                            // wired to llm_calls in Phase 1.5
	}
	prH := &prwriter.Handler{Composer: composer}

	ingestH := &ingestion.Handler{}

	spikeH := &billing.SpikeHandler{Dispatcher: noopSpikeDispatcher{}}

	authH := &auth.Handler{Signer: buildSessionSigner()}
	onboardingH := &onboarding.Handler{Service: onboarding.NewService(onboarding.NewInMemoryStore())}

	return &domainDeps{
		Sandbox:    sandboxH,
		Receipts:   receiptsH,
		PRWriter:   prH,
		Ingest:     ingestH,
		Spike:      spikeH,
		Auth:       authH,
		Onboarding: onboardingH,
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
func mountDomainRoutes(mux *http.ServeMux, deps *domainDeps) {
	// Bypass Handler.Mount so the limiter gets in front of the unauth
	// sandbox routes. FailOpen so a limiter blip can't 503 the sandbox.
	sandboxMW := ratelimit.Middleware(ratelimit.Options{
		Limiter:  ratelimit.NewMemory(sandboxRateLimit, sandboxRateWindow),
		FailOpen: true,
	})
	mux.Handle("POST /v1/analyze", sandboxMW(http.HandlerFunc(deps.Sandbox.Analyze)))
	mux.Handle("GET /r/{hash}", sandboxMW(http.HandlerFunc(deps.Sandbox.Share)))

	deps.Receipts.Mount(mux)
	deps.Ingest.Mount(mux)
	deps.Spike.Mount(mux)
	deps.Auth.Mount(mux)

	tenantMW := func(next http.Handler) http.Handler {
		return requireTenant(HeaderTenantExtractor, next)
	}
	mux.Handle("POST /v1/apply-fixes", tenantMW(http.HandlerFunc(deps.PRWriter.Preview)))
	mux.Handle("GET /v1/onboarding/state", tenantMW(http.HandlerFunc(deps.Onboarding.GetState)))
	mux.Handle("POST /v1/onboarding/transition", tenantMW(http.HandlerFunc(deps.Onboarding.Transition)))
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
			{Method: "GET", Path: "/healthz", Notes: "liveness"},
			{Method: "GET", Path: "/readyz", Notes: "readiness"},
			{Method: "GET", Path: "/metrics", Notes: "prometheus metrics"},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
