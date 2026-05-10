package receipts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"sync"

	"github.com/optiqor/backend/internal/tenancy"
)

// Store is the persistence seam for issued receipts. Production wires
// the receipts table behind it; tests + dev mode use [InMemoryStore].
type Store interface {
	Save(ctx context.Context, t tenancy.Context, id string, signed string, r Receipt) error
	Get(ctx context.Context, id string) (signed string, r Receipt, err error)
}

// ErrReceiptNotFound is returned by Store.Get when the id is unknown.
var ErrReceiptNotFound = errors.New("receipts: not found")

// InMemoryStore is the in-process [Store] used by tests and dev. Safe
// for concurrent use.
type InMemoryStore struct {
	mu sync.RWMutex
	m  map[string]entry
}

type entry struct {
	Signed  string
	Receipt Receipt
}

// NewInMemoryStore returns an empty store.
func NewInMemoryStore() *InMemoryStore { return &InMemoryStore{m: map[string]entry{}} }

// Save stores the receipt under id, overwriting any prior entry.
func (s *InMemoryStore) Save(_ context.Context, _ tenancy.Context, id, signed string, r Receipt) error {
	if id == "" {
		return errors.New("receipts: empty id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = entry{Signed: signed, Receipt: r}
	return nil
}

// Get returns the signed + parsed receipt or ErrReceiptNotFound.
func (s *InMemoryStore) Get(_ context.Context, id string) (string, Receipt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.m[id]
	if !ok {
		return "", Receipt{}, ErrReceiptNotFound
	}
	return e.Signed, e.Receipt, nil
}

// Handler serves GET /v1/receipts/{id}. Public, unauth: a Receipt is
// designed to be independently verifiable.
type Handler struct {
	Store    Store
	Registry Registry
}

// VerifyResponse is the JSON envelope returned to the verifier.
type VerifyResponse struct {
	Receipt        Receipt `json:"receipt"`
	Signed         string  `json:"signed"`
	Verified       bool    `json:"verified"`
	IssuerKeyID    string  `json:"issuer_key_id"`
	VerifierNotice string  `json:"verifier_notice"`
}

// Get returns the stored receipt with a freshly-computed verification
// flag so the caller knows whether the Optiqor server itself still
// trusts the signature today.
//
//   400 — missing id
//   404 — unknown id
//   500 — store / registry failures
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	signed, parsed, err := h.Store.Get(r.Context(), id)
	if errors.Is(err, ErrReceiptNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "store: "+err.Error(), http.StatusInternalServerError)
		return
	}
	resp := VerifyResponse{
		Receipt:        parsed,
		Signed:         signed,
		IssuerKeyID:    parsed.IssuerKeyID,
		VerifierNotice: "Verification keys are published at https://optiqor.dev/keys",
	}
	if h.Registry != nil {
		if _, vErr := Verify(signed, h.Registry); vErr == nil {
			resp.Verified = true
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// Verify serves GET /v/{id} — the public Receipt verifier page.
// Renders an HTML document with the parsed claim, the signed bytes,
// and the live verification status against the configured Registry.
// Anonymous, no auth, no telemetry. The verifier client-side then
// re-verifies with WebCrypto so users don't have to trust this
// server-side check alone.
func (h *Handler) Verify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	signed, parsed, err := h.Store.Get(r.Context(), id)
	if errors.Is(err, ErrReceiptNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "store: "+err.Error(), http.StatusInternalServerError)
		return
	}

	verified := false
	if h.Registry != nil {
		if _, vErr := Verify(signed, h.Registry); vErr == nil {
			verified = true
		}
	}

	// Pretty-print the canonical JSON payload so the verifier page
	// shows the signed bytes alongside the signature for offline
	// re-verification.
	pretty, _ := json.MarshalIndent(parsed, "", "  ")
	sigPart, payloadPart, _ := strings.Cut(signed, ".")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = verifyTmpl.Execute(w, verifyView{
		ID:           parsed.ID,
		TenantID:     parsed.TenantID,
		Workload:     parsed.Workload,
		ApplyFixID:   parsed.ApplyFixID,
		Observed:     fmt.Sprintf("%s → %s", parsed.ObservedFromUTC.Format("2006-01-02"), parsed.ObservedToUTC.Format("2006-01-02")),
		Predicted:    fmtCentsUSD(parsed.PredictedSavingsUSDCents),
		Realised:     fmtCentsUSD(parsed.RealisedSavingsUSDCents),
		CloudBill:    parsed.CloudBillSource,
		IssuerKeyID:  parsed.IssuerKeyID,
		IssuedAtUTC:  parsed.IssuedAtUTC.Format("2006-01-02 15:04 UTC"),
		Verified:     verified,
		Signature:    sigPart,
		PayloadB64:   payloadPart,
		PayloadJSON:  string(pretty),
		Disclosure:   "Agent accuracy: ±15%. Backed by 30 days of Prometheus + your AWS bill.",
	})
}

// Mount registers both the JSON and HTML routes on a mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/receipts/{id}", h.Get)
	mux.HandleFunc("GET /v/{id}", h.Verify)
}

// verifyView is the html/template input. Kept small + flat.
type verifyView struct {
	ID, TenantID, Workload, ApplyFixID, Observed string
	Predicted, Realised, CloudBill                string
	IssuerKeyID, IssuedAtUTC                       string
	Verified                                       bool
	Signature, PayloadB64, PayloadJSON             string
	Disclosure                                     string
}

// fmtCentsUSD renders int64 cents as `$X,YYY.ZZ`. Negative receipts
// shouldn't exist but we handle them defensively.
func fmtCentsUSD(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	d := cents / 100
	rem := cents % 100
	return fmt.Sprintf("%s$%s.%02d", sign, withCommas(d), rem)
}

func withCommas(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	rem := len(s) % 3
	if rem > 0 {
		b.WriteString(s[:rem])
		if len(s) > rem {
			b.WriteString(",")
		}
	}
	for i := rem; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteString(",")
		}
	}
	return b.String()
}

// verifyTmpl is the verifier page. Self-contained HTML, inline CSS,
// same Editorial × Engineering visual language as pkg/htmlrender.
var verifyTmpl = template.Must(template.New("verify").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="robots" content="noindex">
  <title>Receipt {{.ID}} · Optiqor</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Geist:wght@400;500;600&family=Geist+Mono:wght@400;500&display=swap">
  <style>
    :root {
      --ink-0:#0A0A0B; --ink-1:#111114; --ink-2:#16161A; --ink-3:#1C1C22;
      --ink-6:#5C5C68; --ink-7:#9C9CA8; --ink-8:#C8C8D0; --ink-9:#EDEDEE;
      --accent:#22D3EE; --ok:#34D399; --high:#FF6B6B; --med:#F59E0B;
      --rule:#1F1F25; --rule-default:#2A2A33;
      --font-sans: Geist, ui-sans-serif, system-ui, -apple-system, sans-serif;
      --font-mono: "Geist Mono", ui-monospace, SFMono-Regular, monospace;
    }
    * { box-sizing:border-box; }
    body {
      margin:0; background:var(--ink-0); color:var(--ink-9);
      font-family:var(--font-sans); -webkit-font-smoothing:antialiased;
      background-image:
        linear-gradient(to right, rgba(255,255,255,.015) 1px, transparent 1px),
        linear-gradient(to bottom, rgba(255,255,255,.015) 1px, transparent 1px);
      background-size:64px 64px; min-height:100vh;
    }
    a { color:var(--accent); text-decoration:none; }
    a:hover { text-decoration:underline; text-underline-offset:2px; }
    .wrap { max-width:880px; margin:0 auto; padding:56px 32px 96px; }
    .topline { display:flex; align-items:center; justify-content:space-between;
               padding-bottom:18px; border-bottom:1px solid var(--rule); margin-bottom:32px; }
    .brand-text { font-family:var(--font-mono); font-size:12px; letter-spacing:.18em; text-transform:uppercase; }
    .verified, .unverified {
      display:inline-flex; align-items:center; gap:8px;
      font-family:var(--font-mono); font-size:11px; letter-spacing:.1em;
      padding:5px 12px; border-radius:999px;
    }
    .verified  { background:rgba(52,211,153,.12); color:var(--ok); box-shadow:inset 0 0 0 1px rgba(52,211,153,.4); }
    .unverified { background:rgba(255,107,107,.12); color:var(--high); box-shadow:inset 0 0 0 1px rgba(255,107,107,.4); }
    .verified .dot, .unverified .dot { width:6px; height:6px; border-radius:999px; background:currentColor; }
    .eyebrow { font-family:var(--font-mono); font-size:11px; letter-spacing:.14em;
               text-transform:uppercase; color:var(--ink-7); margin-bottom:14px; }
    h1 { font-size:clamp(28px,4.4vw,44px); line-height:1.06; letter-spacing:-.02em; font-weight:500; margin:0; }
    h1 .muted { color:var(--ink-7); }
    .hero { margin-bottom:36px; }
    .hero .id { font-family:var(--font-mono); font-size:14px; color:var(--accent); margin-top:8px; }
    .grid {
      display:grid; grid-template-columns:repeat(2, 1fr); gap:1px;
      background:var(--rule); box-shadow: inset 0 0 0 1px var(--rule-default);
      border-radius:10px; overflow:hidden;
    }
    @media (max-width:640px) { .grid { grid-template-columns:1fr; } }
    .cell { background:var(--ink-1); padding:18px 20px; }
    .cell .l { font-family:var(--font-mono); font-size:10px; letter-spacing:.12em;
               text-transform:uppercase; color:var(--ink-6); }
    .cell .v { margin-top:4px; font-size:15px; color:var(--ink-9); }
    .cell .v.mono { font-family:var(--font-mono); font-size:13px; }
    .cell .v.amt { font-family:var(--font-mono); font-variant-numeric:tabular-nums; font-size:22px; letter-spacing:-.02em; color:var(--ok); }
    .section-title { margin:48px 0 14px; font-family:var(--font-mono); font-size:11px;
                     letter-spacing:.12em; text-transform:uppercase; color:var(--ink-7); }
    pre.code {
      background:var(--ink-2); padding:18px 20px; border-radius:6px;
      box-shadow: inset 0 0 0 1px var(--rule-default);
      font-family:var(--font-mono); font-size:12px; line-height:1.7; color:var(--ink-8);
      overflow-x:auto; white-space:pre; margin:0;
    }
    pre.sig { word-break:break-all; white-space:pre-wrap; color:var(--accent); }
    .accuracy { margin-top:56px; padding-top:24px; border-top:1px solid var(--rule);
                font-family:var(--font-mono); font-size:12px; color:var(--ink-7); line-height:1.6; }
    .footer-meta { margin-top:24px; font-family:var(--font-mono); font-size:11px;
                   letter-spacing:.08em; color:var(--ink-6); display:flex; justify-content:space-between; gap:8px; flex-wrap:wrap; }
  </style>
</head>
<body>
  <main class="wrap">
    <div class="topline">
      <span class="brand-text">Optiqor · Verified Receipt</span>
      {{ if .Verified -}}
        <span class="verified"><span class="dot"></span>signature verified</span>
      {{- else -}}
        <span class="unverified"><span class="dot"></span>signature failed verification</span>
      {{- end }}
    </div>

    <div class="hero">
      <p class="eyebrow">Verified Receipt</p>
      <h1>
        Saved <span class="muted">{{.Realised}}</span><br>
        on workload <span style="color:var(--accent)">{{.Workload}}</span>
      </h1>
      <p class="id">{{.ID}}</p>
    </div>

    <div class="grid">
      <div class="cell"><div class="l">Predicted</div><div class="v amt">{{.Predicted}}</div></div>
      <div class="cell"><div class="l">Realised</div><div class="v amt">{{.Realised}}</div></div>
      <div class="cell"><div class="l">Observation window</div><div class="v mono">{{.Observed}}</div></div>
      <div class="cell"><div class="l">Cloud bill source</div><div class="v mono">{{.CloudBill}}</div></div>
      <div class="cell"><div class="l">Tenant</div><div class="v mono">{{.TenantID}}</div></div>
      <div class="cell"><div class="l">Apply Fix ID</div><div class="v mono">{{.ApplyFixID}}</div></div>
      <div class="cell"><div class="l">Issuer key</div><div class="v mono">{{.IssuerKeyID}}</div></div>
      <div class="cell"><div class="l">Issued (UTC)</div><div class="v mono">{{.IssuedAtUTC}}</div></div>
    </div>

    <h3 class="section-title">Signed payload (canonical JSON)</h3>
    <pre class="code">{{.PayloadJSON}}</pre>

    <h3 class="section-title">Signature (Ed25519, base64url)</h3>
    <pre class="code sig">{{.Signature}}</pre>

    <div class="accuracy">
      <strong style="color:var(--ink-9); font-weight:500">{{.Disclosure}}</strong><br>
      To verify offline: fetch <a href="https://optiqor.dev/keys">/keys</a>,
      then verify <code style="color:var(--ink-9)">signed = signature + "." + payload</code>
      with <code style="color:var(--ink-9)">ed25519.Verify</code>.
    </div>

    <div class="footer-meta">
      <span>Public verifier · no auth · no telemetry</span>
      <span><a href="https://optiqor.dev">optiqor.dev</a></span>
    </div>
  </main>
</body>
</html>`))
