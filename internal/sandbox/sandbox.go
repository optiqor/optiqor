// Package sandbox is the public unauthenticated analysis surface
// behind optiqor.dev/sandbox.
//
// The two HTTP handlers in this package are the operational mirror of
// the CLI's offline `optiqor analyze`:
//
//	POST /v1/analyze    — take a values.yaml body, return findings + savings
//	GET  /r/{hash}      — fetch a previously-shared sanitised analysis
//
// Both ship the mandatory ±40% accuracy disclosure. The handler never
// reads tenant context — the surface is intentionally unauth.
//
// Implementation notes:
//
//   - Parser, detector library, and cost engine all live in their own
//     packages; this file is a thin HTTP shell that composes them.
//   - Body size is capped at 1MiB to bound the cost of a single
//     request (paid sandbox traffic is metered separately).
//   - The shared store is plugged in via interface so the unit tests
//     don't need a database.
//   - Content-hash is sha256 over the sanitised JSON body — same
//     hash function the CLI uses for --share so URLs collide.
package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/htmlrender"
	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/cost"
	"github.com/optiqor/optiqor/internal/parser"
)

// AccuracyDisclosure is the mandatory ±40% line. Keep byte-identical
// to the CLI string.
const AccuracyDisclosure = "Sandbox accuracy: ±40%. Install the Optiqor agent for exact numbers (optiqor.dev/get)."

// MaxBodyBytes caps the size of an /v1/analyze request body.
const MaxBodyBytes = 1 << 20 // 1 MiB

// ShareTTL is how long a /r/<hash> entry stays fetchable. Long enough
// to share in a PR comment and review next morning.
const ShareTTL = 30 * 24 * time.Hour

// Handler holds the dependencies needed to serve both routes. Pricer
// and Region come from cmd/api config; Now is abstracted so tests
// can pin time. PublicBaseURL builds the share_url surfaced to clients;
// when empty the handler derives it from the request (Host header +
// X-Forwarded-Proto), so dev defaults to http://localhost:3000/r/...
// without any wiring.
type Handler struct {
	Store         Store
	Detectors     []rules.Detector // defaults to rules.All() when empty
	Pricer        cost.Pricer
	Region        string
	Now           func() time.Time
	PublicBaseURL string // e.g. "https://optiqor.dev" in prod; "" in dev
}

// AnalyzeResponse is the JSON shape returned by POST /v1/analyze.
// Mirrors the CLI's JSON output so the same client library can
// consume both.
type AnalyzeResponse struct {
	AccuracyDisclosure    string          `json:"accuracy_disclosure"`
	Source                string          `json:"source"`
	Workloads             int             `json:"workloads_analyzed"`
	Findings              []rules.Finding `json:"findings"`
	CostFindings          []rules.Finding `json:"cost_findings"`
	SecurityFindingsBonus []rules.Finding `json:"security_findings_bonus"`
	MonthlySavingsUSD     float64         `json:"monthly_savings_usd"`
	AnnualSavingsUSD      float64         `json:"annual_savings_usd"`
	CostEstimates         []cost.Estimate `json:"cost_estimates,omitempty"`
	ShareHash             string          `json:"share_hash"`
	ShareURL              string          `json:"share_url"`
}

// Analyze parses the body and runs the deterministic rule engine.
//
//	400 — malformed YAML / empty body
//	413 — body exceeds MaxBodyBytes
//	500 — pricer / store failure
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		// MaxBytesReader returns its own error type whose Error() string
		// starts with "http: request body too large".
		if strings.Contains(err.Error(), "request body too large") {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	ws, err := parser.ParseValues(strings.NewReader(string(body)))
	if err != nil {
		if errors.Is(err, parser.ErrParse) {
			http.Error(w, "parser: "+err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "parser: "+err.Error(), http.StatusInternalServerError)
		return
	}

	dets := h.Detectors
	if len(dets) == 0 {
		dets = rules.All()
	}
	findings := rules.Run(ws, dets)

	// Cost estimates per-workload (best effort; never block the response).
	var costEsts []cost.Estimate
	if h.Pricer != nil && h.Region != "" {
		est := &cost.Estimator{Pricer: h.Pricer, Region: h.Region, AccuracyBandPct: 40}
		costEsts = make([]cost.Estimate, 0, len(ws))
		for _, wl := range ws {
			e, err := est.Estimate(wl)
			if err != nil {
				http.Error(w, "cost: "+err.Error(), http.StatusInternalServerError)
				return
			}
			costEsts = append(costEsts, e)
		}
	}

	costF, secF := splitByCategory(findings)
	resp := AnalyzeResponse{
		AccuracyDisclosure:    AccuracyDisclosure,
		Source:                "sandbox",
		Workloads:             len(ws),
		Findings:              findings,
		CostFindings:          costF,
		SecurityFindingsBonus: secF,
		CostEstimates:         costEsts,
		MonthlySavingsUSD:     float64(totalSavingsCents(findings)) / 100,
		AnnualSavingsUSD:      float64(totalSavingsCents(findings)*12) / 100,
	}

	// Hash the *canonical* body so two semantically identical inputs
	// land at the same share URL.
	body = canonicalYAML(body)
	hash := hashBytes(body)
	resp.ShareHash = hash
	resp.ShareURL = h.publicURL(r) + "/r/" + hash

	out, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, "encode: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if h.Store != nil {
		now := h.nowOrDefault()
		_ = h.Store.Put(r.Context(), SharedAnalysis{
			Hash:      hash,
			Body:      out,
			MediaType: "application/json",
			Source:    resp.Source,
			Workloads: resp.Workloads,
			Findings:  resp.Findings,
			CreatedAt: now,
			ExpiresAt: now.Add(ShareTTL),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// Share serves GET /r/{hash}. By default it renders a styled HTML
// page via pkg/htmlrender (Apache-2.0 — same renderer the CLI's
// --html flag uses, so local files and share pages render
// byte-identically). With `Accept: application/json` or `?format=json`
// it returns the cached JSON instead.
//
//	200 — share found, body in requested format
//	404 — unknown / expired hash
func (h *Handler) Share(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hash := r.PathValue("hash")
	if hash == "" {
		http.Error(w, "missing hash", http.StatusBadRequest)
		return
	}
	if h.Store == nil {
		http.Error(w, "store not configured", http.StatusInternalServerError)
		return
	}
	sa, err := h.Store.Get(r.Context(), hash)
	if errors.Is(err, ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", sa.MediaType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(sa.Body)
		return
	}

	// Default: HTML render. Re-using pkg/htmlrender keeps the share
	// page and the CLI's local --html report in lockstep.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = htmlrender.Render(w, htmlrender.Data{
		Source:      sa.Source,
		Workloads:   sa.Workloads,
		Findings:    sa.Findings,
		ShareURL:    h.publicURL(r) + "/r/" + sa.Hash,
		Mode:        htmlrender.ModeSandbox,
		GeneratedAt: sa.CreatedAt,
	})
}

func wantsJSON(r *http.Request) bool {
	if r.URL.Query().Get("format") == "json" {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "application/json")
}

// Mount registers both routes on a mux. cmd/api wraps the result with
// its own middleware (request id, access log, panic recovery).
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/analyze", h.Analyze)
	mux.HandleFunc("GET /r/{hash}", h.Share)
}

// Helpers -------------------------------------------------------------

func splitByCategory(in []rules.Finding) (costF, secF []rules.Finding) {
	costF = make([]rules.Finding, 0, len(in))
	secF = make([]rules.Finding, 0, len(in))
	for _, f := range in {
		if f.Category == rules.CategorySecurity {
			secF = append(secF, f)
		} else {
			costF = append(costF, f)
		}
	}
	return
}

func totalSavingsCents(in []rules.Finding) int64 {
	var sum int64
	for _, f := range in {
		sum += f.MonthlyUSDCents
	}
	return sum
}

func hashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:12]) // 96-bit prefix matches CLI --share
}

// canonicalYAML strips trailing whitespace + final newline. The full
// canonicalisation lives in pkg/share on the CLI side; for sandbox
// purposes we only need a stable byte stream for hashing, not
// semantically-equal YAML to collide.
func canonicalYAML(b []byte) []byte {
	// Sort top-level keys is too invasive here — keep it cheap.
	trim := strings.TrimRight(string(b), "\n\t ")
	return []byte(trim)
}

func (h *Handler) nowOrDefault() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
}

// String adds a String() for context propagation logging.
func (h *Handler) String() string {
	return fmt.Sprintf("sandbox.Handler(region=%s)", h.Region)
}

// publicURL returns the user-facing origin for share links. When the
// operator has configured a fixed PublicBaseURL we use it verbatim
// (production), otherwise we derive scheme + host from the inbound
// request — so dev sees http://localhost:3000 automatically via the
// Next.js proxy, and prod-behind-a-reverse-proxy honours
// X-Forwarded-Proto / X-Forwarded-Host.
func (h *Handler) publicURL(r *http.Request) string {
	if h.PublicBaseURL != "" {
		return strings.TrimRight(h.PublicBaseURL, "/")
	}
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		host = fh
	}
	return scheme + "://" + host
}
