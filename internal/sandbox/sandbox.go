// Package sandbox is the unauthenticated analysis surface behind
// optiqor.dev/sandbox. Every response carries the mandatory ±40%
// accuracy disclosure; handlers never read tenant context.
package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/htmlrender"
	"github.com/optiqor/optiqor-cli/pkg/rules"
	"github.com/optiqor/optiqor/internal/cost"
	"github.com/optiqor/optiqor/internal/parser"
	"github.com/optiqor/optiqor/internal/platform/config"
)

// AccuracyDisclosure must stay byte-identical to the CLI string so
// users see the same language end-to-end.
const AccuracyDisclosure = "Sandbox accuracy: ±40%. Install the Optiqor agent for exact numbers (optiqor.dev/get)."

// ShareTTL is long enough to share in a PR comment and review the
// next morning.
const ShareTTL = 30 * 24 * time.Hour

// Handler serves /v1/analyze and /r/{hash}. PublicBaseURL is the
// user-facing origin for share links; when empty the handler derives
// it from the request (Host + X-Forwarded-Proto) so dev gets
// http://localhost:3000/r/... without wiring.
type Handler struct {
	Store         Store
	Detectors     []rules.Detector // defaults to rules.All() when empty
	Pricer        cost.Pricer
	Region        string
	Now           func() time.Time
	PublicBaseURL string
	// Logger records share-store errors. Nil-safe; defaults to slog.Default().
	Logger *slog.Logger
}

// AnalyzeResponse mirrors the CLI's JSON output so the same client
// library consumes both.
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
// 400 on malformed YAML, 413 above config.SandboxAnalyzeMaxBytes,
// 500 on pricer/store failure.
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, config.SandboxAnalyzeMaxBytes))
	if err != nil {
		// MaxBytesReader's error Error() starts with "http: request body too large".
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

	// Hash the canonical body so byte-identical inputs collide on
	// the same share URL across reanalysis.
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
		if err := h.Store.Put(r.Context(), SharedAnalysis{
			Hash:      hash,
			Body:      out,
			MediaType: "application/json",
			Source:    resp.Source,
			Workloads: resp.Workloads,
			Findings:  resp.Findings,
			CreatedAt: now,
			ExpiresAt: now.Add(ShareTTL),
		}); err != nil {
			// Share-store write failure makes the URL a 404 later.
			// Log loudly + continue so the analyze response still goes
			// back to the caller; the analysis itself stays valid.
			h.logger().Error("sandbox: share store put failed", "hash", hash, "err", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// Share serves GET /r/{hash}. Default renders HTML via pkg/htmlrender
// (same renderer the CLI's --html flag uses, so share pages and local
// reports stay byte-identical). `Accept: application/json` or
// `?format=json` returns the cached JSON instead. 404 covers both
// missing and expired hashes.
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

// Mount registers both routes; cmd/api wraps the mux with request id,
// access log, and panic recovery middleware.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/analyze", h.Analyze)
	mux.HandleFunc("GET /r/{hash}", h.Share)
}

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
	// 96-bit prefix matches the CLI's --share hash so URLs collide.
	return hex.EncodeToString(h[:12])
}

// canonicalYAML only trims trailing whitespace; the full canonicalisation
// lives in pkg/share on the CLI side. A stable byte stream is enough
// for hashing — we don't need semantically-equal YAML to collide.
func canonicalYAML(b []byte) []byte {
	trim := strings.TrimRight(string(b), "\n\t ")
	return []byte(trim)
}

func (h *Handler) nowOrDefault() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
}

func (h *Handler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func (h *Handler) String() string {
	return fmt.Sprintf("sandbox.Handler(region=%s)", h.Region)
}

// publicURL honours PublicBaseURL when set; otherwise derives scheme
// and host from the request so dev works without wiring and prod
// behind a reverse proxy picks up X-Forwarded-Proto / X-Forwarded-Host.
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
