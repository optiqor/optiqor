package analyze

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/optiqor/optiqor-cli/internal/render"
	"github.com/optiqor/optiqor-cli/internal/render/style"
	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// WriteText renders the CompareReport as a human-readable side-by-side
// comparison. The accuracy disclosure is mandatory (CLAUDE.md hard rule).
func (r CompareReport) WriteText(w io.Writer, opts render.Options) error {
	t := style.NewTheme(opts.Color)
	width := opts.Width
	if width <= 0 {
		width = 80
	}

	var b strings.Builder

	//  Header
	fmt.Fprintf(&b, "%s\n", t.DividerLine(width))
	label := fmt.Sprintf("compare: %s  vs  %s", shortPath(r.A), shortPath(r.B))
	fmt.Fprintf(&b, "%s\n", t.SectionRule(label, width, t.SectionPrimary))
	fmt.Fprintf(&b, "%s\n\n", t.DividerLine(width))

	//  Cost summary
	aLabel := t.Workload.Render(shortPath(r.A))
	bLabel := t.Workload.Render(shortPath(r.B))

	fmt.Fprintf(&b, "  %s   %s\n", t.Muted.Render("Chart A:"), aLabel)
	fmt.Fprintf(&b, "  %s   %s\n\n", t.Muted.Render("Chart B:"), bLabel)

	fmt.Fprintf(&b, "  %s\n", t.Muted.Render("Estimated monthly cost (±40%):"))
	fmt.Fprintf(&b, "    %s  %s\n", t.Muted.Render("A:"), t.BigSavings.Render("$"+formatCents(r.CostA)+"/mo"))
	fmt.Fprintf(&b, "    %s  %s\n\n", t.Muted.Render("B:"), t.BigSavings.Render("$"+formatCents(r.CostB)+"/mo"))

	//  Winner
	switch r.Winner {
	case "a":
		savings := r.CostB - r.CostA
		fmt.Fprintf(&b, "  %s %s  saves ~%s/mo vs B  (±40%%)\n\n",
			t.OK.Render("✓ Winner: A"),
			t.Muted.Render("—"),
			t.Savings.Render("$"+formatCents(savings)),
		)
	case "b":
		savings := r.CostA - r.CostB
		fmt.Fprintf(&b, "  %s %s  saves ~%s/mo vs A  (±40%%)\n\n",
			t.OK.Render("✓ Winner: B"),
			t.Muted.Render("—"),
			t.Savings.Render("$"+formatCents(savings)),
		)
	default:
		fmt.Fprintf(&b, "  %s\n\n", t.Muted.Render("✓ Tie — identical cost and HIGH findings"))
	}

	//  Findings only in A
	writeCompareSection(&b, t, width,
		fmt.Sprintf("Findings only in A (%d)", len(r.OnlyInA)),
		r.OnlyInA,
		t.SectionPrimary,
	)

	//  Findings only in B
	writeCompareSection(&b, t, width,
		fmt.Sprintf("Findings only in B (%d)", len(r.OnlyInB)),
		r.OnlyInB,
		t.SectionBonus,
	)

	//  Findings in both
	writeCompareSection(&b, t, width,
		fmt.Sprintf("Findings in both (%d)", len(r.InBoth)),
		r.InBoth,
		t.SectionSubtle,
	)

	//  Footer
	fmt.Fprintf(&b, "%s\n", t.DividerLine(width))
	fmt.Fprintf(&b, "  %s\n", t.Disclosure.Render(render.AccuracyDisclosure))

	_, err := io.WriteString(w, b.String())
	return err
}

// WriteJSON emits the CompareReport as machine-readable JSON with the
// mandatory accuracy disclosure.
func (r CompareReport) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		AccuracyDisclosure string          `json:"accuracy_disclosure"`
		A                  string          `json:"a"`
		B                  string          `json:"b"`
		CostAMonthlyUSD    float64         `json:"cost_a_monthly_usd"`
		CostBMonthlyUSD    float64         `json:"cost_b_monthly_usd"`
		Winner             string          `json:"winner"`
		OnlyInA            []rules.Finding `json:"only_in_a"`
		OnlyInB            []rules.Finding `json:"only_in_b"`
		InBoth             []rules.Finding `json:"in_both"`
	}{
		AccuracyDisclosure: render.AccuracyDisclosure,
		A:                  r.A,
		B:                  r.B,
		CostAMonthlyUSD:    float64(r.CostA) / 100.0,
		CostBMonthlyUSD:    float64(r.CostB) / 100.0,
		Winner:             r.Winner,
		OnlyInA:            r.OnlyInA,
		OnlyInB:            r.OnlyInB,
		InBoth:             r.InBoth,
	})
}

// writeCompareSection renders one findings group as a labelled section
// with compact one-liners. accent is a lipgloss.Style used for the
// section rule colour.
func writeCompareSection(b *strings.Builder, t style.Theme, width int, label string, findings []rules.Finding, accent lipgloss.Style) {
	b.WriteString("\n")
	fmt.Fprintf(b, "%s\n", t.SectionRule(label, width, accent))
	if len(findings) == 0 {
		fmt.Fprintf(b, "  %s\n", t.Muted.Render("none"))
		return
	}

	// Compute workload column width.
	maxWL := 0
	for _, f := range findings {
		if n := len([]rune(f.Workload)); n > maxWL {
			maxWL = n
		}
	}
	if maxWL > 20 {
		maxWL = 20
	}

	for _, f := range findings {
		wl := f.Workload
		if len([]rune(wl)) > maxWL {
			wl = string([]rune(wl)[:maxWL-1]) + "…"
		}
		wlPadded := wl + strings.Repeat(" ", maxWL-len([]rune(wl)))

		savStr := ""
		if f.MonthlyUSDCents > 0 {
			savStr = "  " + t.Savings.Render("save ~$"+formatCents(f.MonthlyUSDCents)+"/mo")
		}

		title := f.Title
		if title == "" {
			title = f.DetectorID
		}

		fmt.Fprintf(b, "  %s  %s   %s   %s%s\n",
			t.SeverityBadge(string(f.Severity)),
			t.Workload.Render(wlPadded),
			t.ConfidenceGlyph(string(f.Confidence)),
			t.Detail.Render(title),
			savStr,
		)
	}
}

// shortPath trims the path to the last two path components so headers
// stay readable on narrow terminals.
func shortPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return "…/" + strings.Join(parts[len(parts)-2:], "/")
}
