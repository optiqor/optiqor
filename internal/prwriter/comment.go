// Package prwriter renders the Markdown PR comment Optiqor posts on
// every Helm/Kustomize PR, and prepares the Apply Fix PR body.
//
// The renderer is pure functions over [Comment] — no GitHub API calls
// live here. The HTTP/PR opener lives in cmd/api once the GitHub App
// integration ships; until then the rendered comment is exposed via
// POST /v1/apply-fixes for preview.
//
// Style choices baked in:
//
//   - Cost first, security as a bonus (matches the CLI brand voice).
//   - Every comment carries the accuracy disclosure; the sandbox-mode
//     vs agent-mode band is the only thing that varies.
//   - Findings are stable-sorted so the same input always renders the
//     same bytes — diffable in code review.
//
// The mandatory disclosure string MUST match the CLI exactly so users
// see the same language end-to-end. Update both in lockstep.
package prwriter

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// AccuracyDisclosureSandbox matches the CLI verbatim. Don't reflow.
const AccuracyDisclosureSandbox = "Sandbox accuracy: ±40%. Install the Optiqor agent for exact numbers (optiqor.dev/get)."

// AccuracyDisclosureAgent is the agent-mode (paid customer) variant.
const AccuracyDisclosureAgent = "Agent accuracy: ±15%. Backed by 30 days of Prometheus + your AWS bill."

// Mode discriminates which accuracy line a comment renders.
type Mode int

const (
	// ModeSandbox renders the ±40% disclosure (CLI / unauth sandbox).
	ModeSandbox Mode = iota
	// ModeAgent renders the ±15% disclosure (paid agent customer).
	ModeAgent
)

// Comment is what callers build and hand to [Render]. Only public
// fields belong here; rendering logic owns nothing else.
type Comment struct {
	Chart                   string
	Tenant                  string
	Workloads               int
	Findings                []rules.Finding
	MonthlySavingsUSDCents  int64
	AnnualSavingsUSDCents   int64
	Mode                    Mode
	GeneratedAt             time.Time
	OptiqorAnalysisURL      string // e.g. https://optiqor.dev/r/<hash>
	ApplyFixURL             string // populated when an Apply Fix PR has been opened
	// SecurityVisible toggles the bonus security section. Default off;
	// most customers turn it on after they've cleaned up cost first.
	SecurityVisible bool
}

// ErrNoChart is returned when callers forget to set the chart name —
// the rendered comment would be context-free without it.
var ErrNoChart = errors.New("prwriter: chart name required")

// Render returns the Markdown comment. Always pure; never errors for
// reasons beyond invalid input.
func Render(c Comment) (string, error) {
	if c.Chart == "" {
		return "", ErrNoChart
	}
	cost, security := split(c.Findings)
	view := view{
		Chart:                   c.Chart,
		Workloads:               c.Workloads,
		CostFindings:            sortCostForDisplay(cost),
		SecurityFindings:        security,
		ShowSecurity:            c.SecurityVisible && len(security) > 0,
		MonthlyUSD:              fmtUSD(c.MonthlySavingsUSDCents),
		AnnualUSD:               fmtUSD(c.AnnualSavingsUSDCents),
		ShowSavings:             c.MonthlySavingsUSDCents > 0,
		AccuracyDisclosure:      AccuracyDisclosureSandbox,
		OptiqorAnalysisURL:      c.OptiqorAnalysisURL,
		ApplyFixURL:             c.ApplyFixURL,
		GeneratedAtISO:          generatedAt(c.GeneratedAt).Format(time.RFC3339),
	}
	if c.Mode == ModeAgent {
		view.AccuracyDisclosure = AccuracyDisclosureAgent
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, view); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n") + "\n", nil
}

type view struct {
	Chart              string
	Workloads          int
	CostFindings       []rules.Finding
	SecurityFindings   []rules.Finding
	ShowSecurity       bool
	MonthlyUSD         string
	AnnualUSD          string
	ShowSavings        bool
	AccuracyDisclosure string
	OptiqorAnalysisURL string
	ApplyFixURL        string
	GeneratedAtISO     string
}

// generatedAt zeros out sub-second resolution so the rendered comment
// stays diff-stable across CI reruns within the same minute.
func generatedAt(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC().Truncate(time.Minute)
	}
	return t.UTC().Truncate(time.Minute)
}

func split(in []rules.Finding) (cost, sec []rules.Finding) {
	cost = make([]rules.Finding, 0, len(in))
	sec = make([]rules.Finding, 0, len(in))
	for _, f := range in {
		if f.Category == rules.CategorySecurity {
			sec = append(sec, f)
		} else {
			cost = append(cost, f)
		}
	}
	return
}

func sortCostForDisplay(in []rules.Finding) []rules.Finding {
	out := make([]rules.Finding, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.MonthlyUSDCents > 0) != (b.MonthlyUSDCents > 0) {
			return a.MonthlyUSDCents > b.MonthlyUSDCents
		}
		if a.MonthlyUSDCents != b.MonthlyUSDCents {
			return a.MonthlyUSDCents > b.MonthlyUSDCents
		}
		if a.Workload != b.Workload {
			return a.Workload < b.Workload
		}
		return a.DetectorID < b.DetectorID
	})
	return out
}

func fmtUSD(cents int64) string {
	if cents == 0 {
		return "$0"
	}
	dollars := cents / 100
	c := cents % 100
	if c == 0 {
		return fmt.Sprintf("$%d", dollars)
	}
	return fmt.Sprintf("$%d.%02d", dollars, c)
}

const tmplBody = `## Optiqor analysis — {{.Chart}}

{{ if .ShowSavings -}}
**Potential savings:** {{.MonthlyUSD}} / month · ~{{.AnnualUSD}} / year
{{- else -}}
**No cost optimisations detected** — this chart is already clean.
{{- end }}

_Workloads analysed: {{.Workloads}}._

{{ if .CostFindings -}}
### Cost optimisations
| Severity | Workload | Title | Save / mo |
| --- | --- | --- | --- |
{{ range .CostFindings -}}
| {{.Severity}} | {{.Workload}} | {{.Title}} | {{ if gt .MonthlyUSDCents 0 }}save ~${{ printf "%d.%02d" (divCents .MonthlyUSDCents 100) (modCents .MonthlyUSDCents 100) }}{{ else }}—{{ end }} |
{{ end }}
{{- end }}

{{ if .ShowSecurity -}}
### Security findings (bonus)
_Spotted while parsing your chart. Cost is the headline; this is a side-effect._

| Severity | Workload | Title |
| --- | --- | --- |
{{ range .SecurityFindings -}}
| {{.Severity}} | {{.Workload}} | {{.Title}} |
{{ end }}
{{- end }}

---

> {{.AccuracyDisclosure}}

{{ if .OptiqorAnalysisURL -}}
[Full analysis →]({{.OptiqorAnalysisURL}}){{ if .ApplyFixURL }} · [Apply Fix PR →]({{.ApplyFixURL}}){{ end }}
{{ end }}
<sub>Generated at {{.GeneratedAtISO}} by optiqor.dev</sub>
`

var tmpl = template.Must(template.
	New("comment").
	Funcs(template.FuncMap{
		"divCents": func(a, b int64) int64 { return a / b },
		"modCents": func(a, b int64) int64 { return a % b },
	}).
	Parse(tmplBody))
