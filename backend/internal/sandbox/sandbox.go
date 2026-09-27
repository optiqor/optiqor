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
	"html"
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
	"github.com/optiqor/optiqor/internal/platform/httperr"
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
		httperr.MethodNotAllowed(w, r, "POST")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, config.SandboxAnalyzeMaxBytes))
	if err != nil {
		if httperr.IsBodyTooLarge(err) || strings.Contains(err.Error(), "request body too large") {
			httperr.BodyTooLarge(w, r, config.SandboxAnalyzeMaxBytes)
			return
		}
		httperr.BadRequest(w, r, "could not read request body: "+err.Error())
		return
	}
	defer func() { _ = r.Body.Close() }()

	ws, err := parser.ParseValues(strings.NewReader(string(body)))
	if err != nil {
		if errors.Is(err, parser.ErrParse) {
			httperr.WriteWithDetails(w, r, http.StatusBadRequest, "PARSE_ERROR",
				"could not parse values.yaml: "+err.Error(),
				map[string]any{"hint": "make sure the body is a single Helm values.yaml document"})
			return
		}
		httperr.LogAndInternal(w, r, h.logger(), err, "parser")
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
				httperr.LogAndInternal(w, r, h.logger(), err, "cost.estimate")
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
		httperr.LogAndInternal(w, r, h.logger(), err, "json.marshal")
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
		httperr.MethodNotAllowed(w, r, "GET")
		return
	}
	hash := r.PathValue("hash")
	if hash == "" {
		httperr.MissingField(w, r, "hash")
		return
	}
	if h.Store == nil {
		httperr.LogAndInternal(w, r, h.logger(), errors.New("nil store"), "sandbox.Share")
		return
	}
	sa, err := h.Store.Get(r.Context(), hash)
	switch {
	case errors.Is(err, ErrExpired):
		if wantsJSON(r) {
			httperr.Gone(w, r, "shared analysis")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		renderExpiredHTML(w, hash)
		return
	case errors.Is(err, ErrNotFound):
		if wantsJSON(r) {
			httperr.NotFound(w, r, "shared analysis")
			return
		}
		renderNotFoundHTML(w, hash)
		return
	case err != nil:
		httperr.LogAndInternal(w, r, h.logger(), err, "sandbox.Share")
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

// renderNotFoundHTML answers /r/{hash} for an unknown hash with a tiny
// branded page rather than a bare "not found" string — browser users
// hit this when a share URL was mistyped or never existed.
func renderNotFoundHTML(w http.ResponseWriter, hash string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	//nolint:gosec // G705: hash is HTML-escaped inside sharePage via html.EscapeString
	_, _ = w.Write(sharePage("Share not found",
		"We could not find an analysis at this URL. The hash may be mistyped, or the share never existed.",
		hash))
}

// renderExpiredHTML answers /r/{hash} for a past-TTL hash with 410 Gone
// + a friendly message + a CTA to re-run the analysis. 410 (not 404)
// tells caches and crawlers not to retry.
func renderExpiredHTML(w http.ResponseWriter, hash string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusGone)
	//nolint:gosec // G705: hash is HTML-escaped inside sharePage via html.EscapeString
	_, _ = w.Write(sharePage("Share expired",
		"This analysis was shared more than 30 days ago and has aged out. Re-run the analyzer to mint a fresh URL.",
		hash))
}

const sharePageStyle = `<style>
body{font-family:ui-sans-serif,system-ui,sans-serif;max-width:42rem;margin:4rem auto;padding:0 1.25rem;color:#1f2933;line-height:1.55}
h1{margin:0 0 0.5rem;font-size:1.5rem}
.muted{color:#52606d;font-size:0.95rem}
.cta{display:inline-block;margin-top:1.5rem;padding:0.55rem 1rem;background:#111;color:#fff;text-decoration:none;border-radius:6px;font-weight:600}
.hash{font-family:ui-monospace,monospace;font-size:0.85rem;color:#7b8794;margin-top:1.5rem}
</style>`

func sharePage(title, body, hash string) []byte {
	// hash is user-supplied URL input — escape before embedding.
	safeHash := html.EscapeString(hash)
	return []byte("<!doctype html><html><head><meta charset=\"utf-8\"><title>" + title +
		" — Optiqor</title>" + sharePageStyle + "</head><body>" +
		"<h1>" + title + "</h1>" +
		"<p class=\"muted\">" + body + "</p>" +
		"<a class=\"cta\" href=\"/sandbox\">Open the sandbox →</a>" +
		"<div class=\"hash\">share: <code>" + safeHash + "</code></div>" +
		"</body></html>")
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
