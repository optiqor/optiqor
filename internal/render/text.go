// Package render formats analysis results as styled text or JSON.
//
// Every renderer must include the ±40% accuracy disclosure — hard rule
// per ../../CLAUDE.md.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/optiqor/optiqor-cli/internal/render/style"
	"github.com/optiqor/optiqor-cli/pkg/rules"
)

// AccuracyDisclosure is the mandatory line every output must contain
// (hard rule per CLAUDE.md — never soften, never make dismissible).
const AccuracyDisclosure = "Sandbox accuracy: ±40%. Install the Optiqor agent for exact numbers (optiqor.dev/get)."

// Brand strings surfaced in the header banner.
const (
	BrandName    = "optiqor"
	BrandTagline = "Helm chart cost optimization · security as a bonus"
	GetURL       = "https://optiqor.dev/get"
)

const (
	defaultWidth     = 78
	contentIndent    = "  "
	findingIndent    = "    "
	monthsPerYear    = 12
	annualTeaserMin  = 1_00 // show annual projection only above $1/mo savings
	bonusSectionName = "Security findings"
	costSectionName  = "Cost optimizations"

	// cardMinInner: below this the boxed layout breaks down, so we
	// fall back to a flat rendering on very narrow terminals.
	cardMinInner = 50

	signalBarWidth = 24
)

// Report is the renderer-facing view of an analysis run.
type Report struct {
	Source    string          `json:"source"`
	Workloads int             `json:"workloads_analyzed"`
	Findings  []rules.Finding `json:"findings"`
}

// Options controls how a Report is rendered.
type Options struct {
	Color bool
	Width int // 0 → defaultWidth

	// Roast strings are supplied by the caller rather than imported
	// from internal/roast so this package stays leaf-level.
	Roast        bool
	RoastTagline string
	RoastFooter  string
}

// MonthlySavingsUSDCents totals the predicted savings across findings.
func (r Report) MonthlySavingsUSDCents() int64 {
	var sum int64
	for _, f := range r.Findings {
		sum += f.MonthlyUSDCents
	}
	return sum
}

// Text writes the terminal-friendly report. Always includes the
// AccuracyDisclosure footer (hard rule per CLAUDE.md).
func Text(w io.Writer, r Report, opts Options) error {
	t := style.NewTheme(opts.Color)
	width := opts.Width
	if width <= 0 {
		width = defaultWidth
	}

	cost, security := splitByCategory(r.Findings)

	var b strings.Builder
	writeHeader(&b, t, width, opts)
	writeSummary(&b, t, r, len(cost), len(security))

	if len(cost) == 0 && len(security) == 0 {
		fmt.Fprintf(&b, "\n%s%s\n\n", contentIndent, t.OK.Render("✓ Clean. No findings."))
		writeFooter(&b, t, width, 0, opts)
		_, err := io.WriteString(w, b.String())
		return err
	}

	if len(cost) > 0 {
		writeCostSection(&b, t, width, sortCostForDisplay(cost))
	}
	if len(security) > 0 {
		writeSecuritySection(&b, t, width, security)
	}

	writeFooter(&b, t, width, r.MonthlySavingsUSDCents(), opts)
	_, err := io.WriteString(w, b.String())
	return err
}

// sortCostForDisplay reorders cost findings so the biggest dollar
// impact leads. The engine's stable workload→severity sort is right
// for diffs/audit but buries high-savings behind alphabetically
// earlier workloads. Returns a new slice; input is not mutated.
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
		if a.Severity != b.Severity {
			return severityRank(a.Severity) > severityRank(b.Severity)
		}
		if a.Workload != b.Workload {
			return a.Workload < b.Workload
		}
		return a.DetectorID < b.DetectorID
	})
	return out
}

func severityRank(s rules.Severity) int {
	switch s {
	case rules.SeverityHigh:
		return 3
	case rules.SeverityMed:
		return 2
	case rules.SeverityLow:
		return 1
	}
	return 0
}

// splitByCategory partitions findings preserving rules.Run order
// (workload → severity → detector ID). Cost is the headline section
// and security is bonus; uncategorised findings fall through to cost
// so a detector that forgets to declare Category stays visible.
func splitByCategory(findings []rules.Finding) (cost, security []rules.Finding) {
	cost = make([]rules.Finding, 0, len(findings))
	security = make([]rules.Finding, 0, len(findings))
	for _, f := range findings {
		switch f.Category {
		case rules.CategorySecurity:
			security = append(security, f)
		default:
			cost = append(cost, f)
		}
	}
	return cost, security
}

func writeHeader(b *strings.Builder, t style.Theme, width int, opts Options) {
	div := t.DividerLine(width)
	mark := t.BrandMark.Render(style.BrandGlyph)
	brand := t.Brand.Render(BrandName)
	taglineText := BrandTagline
	if opts.Roast && opts.RoastTagline != "" {
		taglineText = opts.RoastTagline
	}
	tag := t.Tagline.Render(taglineText)
	fmt.Fprintf(b, "%s\n", div)
	fmt.Fprintf(b, "%s%s  %s\n", contentIndent, mark, brand)
	fmt.Fprintf(b, "%s%s\n", contentIndent, tag)
	fmt.Fprintf(b, "%s\n\n", div)
}

func writeSummary(b *strings.Builder, t style.Theme, r Report, costCount, secCount int) {
	srcLabel := r.Source
	if srcLabel == "" {
		srcLabel = "(stdin)"
	}
	monthly := r.MonthlySavingsUSDCents()

	rows := [][2]string{
		{"Source", t.Workload.Render(srcLabel)},
		{"Workloads", t.Title.Render(plural(r.Workloads, "workload", "workloads")) + t.Muted.Render(" analyzed")},
		{"Cost", costSummaryValue(t, costCount, monthly)},
	}
	if secCount > 0 {
		rows = append(rows, [2]string{
			"Security",
			t.Muted.Render(plural(secCount, "finding", "findings") + " — bonus, surfaced while parsing"),
		})
	}

	labelWidth := 0
	for _, row := range rows {
		if n := len([]rune(row[0])); n > labelWidth {
			labelWidth = n
		}
	}
	for _, row := range rows {
		label := row[0] + strings.Repeat(" ", labelWidth-len([]rune(row[0])))
		fmt.Fprintf(b, "%s%s   %s\n", contentIndent, t.Muted.Render(label), row[1])
	}
}

func costSummaryValue(t style.Theme, count int, monthlyCents int64) string {
	if count == 0 {
		return t.OK.Render("✓ no cost waste detected")
	}
	primary := t.Title.Render(plural(count, "optimization", "optimizations"))
	if monthlyCents <= 0 {
		return primary
	}
	monthly := t.BigSavings.Render("save ~$" + formatCents(monthlyCents) + "/mo")
	suffix := ""
	if monthlyCents >= annualTeaserMin {
		suffix = t.Muted.Render(fmt.Sprintf(" (~$%s/yr)", formatCents(monthlyCents*monthsPerYear)))
	}
	return primary + t.Muted.Render(" · ") + monthly + suffix + t.Muted.Render(" ±40%")
}

func writeCostSection(b *strings.Builder, t style.Theme, width int, findings []rules.Finding) {
	b.WriteString("\n")
	fmt.Fprintf(b, "%s\n\n", t.SectionRule(costSectionName, width, t.SectionPrimary))
	for i, f := range findings {
		writeCostFinding(b, t, f, width)
		if i < len(findings)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
}

// writeCostFinding renders a finding as a boxed card; degrades to a
// flat layout below cardMinInner inner width.
func writeCostFinding(b *strings.Builder, t style.Theme, f rules.Finding, width int) {
	// Card body cells = innerWidth + 4 — see writeCardHeaderRule for
	// the breakdown ("│ " on the left, " │" on the right) — so with the
	// indent the card is exactly width columns and never wraps.
	innerWidth := width - len(contentIndent) - 4
	if innerWidth < cardMinInner {
		writeCostFindingFlat(b, t, f, width)
		return
	}

	title := f.Title
	if title == "" && f.DetectorID != "" {
		title = f.DetectorID
	}

	// Header line: ┌─ HIGH · workload ──────── save ~$29/mo ─┐
	left := fmt.Sprintf(" %s · %s ",
		strings.TrimSpace(string(f.Severity)),
		f.Workload,
	)
	right := ""
	if f.MonthlyUSDCents > 0 {
		right = " save ~$" + formatCents(f.MonthlyUSDCents) + "/mo "
	}
	writeCardHeaderRule(b, t, f.Severity, left, right, innerWidth)

	cardLine(b, t, innerWidth, "")
	cardLine(b, t, innerWidth, t.Title.Render(title))
	if f.Signal != nil {
		cardLine(b, t, innerWidth, "")
		cardLine(b, t, innerWidth, formatSignalLine(t, *f.Signal))
	}
	cardLine(b, t, innerWidth, "")
	for _, line := range wrap(f.Detail, innerWidth-2) {
		cardLine(b, t, innerWidth, t.Detail.Render(line))
	}
	cardLine(b, t, innerWidth, "")
	cardLine(b, t, innerWidth, fmt.Sprintf("%s %s",
		t.Muted.Render("confidence:"),
		t.ConfidenceDots(string(f.Confidence)),
	))

	// Bottom rule
	fmt.Fprintf(b, "%s%s\n", contentIndent,
		t.CardBorder.Render("└"+strings.Repeat("─", innerWidth+2)+"┘"),
	)
}

// writeCostFindingFlat: narrow-terminal fallback, identical content
// with no box.
func writeCostFindingFlat(b *strings.Builder, t style.Theme, f rules.Finding, width int) {
	badge := t.SeverityBadge(string(f.Severity))
	wl := t.Workload.Render(f.Workload)
	if f.MonthlyUSDCents > 0 {
		savings := t.Savings.Render("save ~$" + formatCents(f.MonthlyUSDCents) + "/mo")
		fmt.Fprintf(b, "%s%s  %s   %s\n", contentIndent, badge, wl, savings)
	} else {
		fmt.Fprintf(b, "%s%s  %s\n", contentIndent, badge, wl)
	}
	title := f.Title
	if title == "" && f.DetectorID != "" {
		title = f.DetectorID
	}
	fmt.Fprintf(b, "%s%s\n", findingIndent, t.Title.Render(title))
	if f.Signal != nil {
		fmt.Fprintf(b, "%s%s\n", findingIndent, formatSignalLine(t, *f.Signal))
	}
	for _, line := range wrap(f.Detail, width-len(findingIndent)) {
		fmt.Fprintf(b, "%s%s\n", findingIndent, t.Detail.Render(line))
	}
	fmt.Fprintf(b, "%s%s %s\n", findingIndent,
		t.Muted.Render("confidence:"),
		t.ConfidenceDots(string(f.Confidence)),
	)
}

// writeCardHeaderRule writes the top of a card with embedded labels.
// Sized to match cardLine's "│ " + innerWidth + " │" so the edges
// align — keep in sync if cardLine's framing changes.
func writeCardHeaderRule(b *strings.Builder, t style.Theme, sev rules.Severity, left, right string, innerWidth int) {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	// (innerWidth+4) cells - 2 corners - 2 lead/trail dashes = innerWidth.
	usable := innerWidth
	if len(leftRunes)+len(rightRunes) > usable {
		maxLeft := usable - len(rightRunes)
		if maxLeft < 4 {
			maxLeft = 4
		}
		leftRunes = leftRunes[:maxLeft]
	}
	gap := usable - len(leftRunes) - len(rightRunes)
	if gap < 1 {
		gap = 1
	}

	leftStyled := stylizeSeverityWord(t, sev, string(leftRunes))
	gapStr := strings.Repeat("─", gap)
	right = string(rightRunes)

	fmt.Fprintf(b, "%s%s%s%s%s%s\n",
		contentIndent,
		t.CardBorder.Render("┌─"),
		leftStyled,
		t.CardBorder.Render(gapStr),
		t.Savings.Render(right),
		t.CardBorder.Render("─┐"),
	)
}

// stylizeSeverityWord recolours just the leading severity token in
// the card header label; the workload name keeps the border tone.
func stylizeSeverityWord(t style.Theme, sev rules.Severity, label string) string {
	parts := strings.SplitN(label, " · ", 2)
	if len(parts) != 2 {
		return t.CardBorder.Render(label)
	}
	sevTok := parts[0]
	rest := " · " + parts[1]
	var sevStyle = t.Muted
	switch sev {
	case rules.SeverityHigh:
		sevStyle = t.SevHigh
	case rules.SeverityMed:
		sevStyle = t.SevMed
	case rules.SeverityLow:
		sevStyle = t.SevLow
	}
	// Inside a header rule the chip becomes plain text in the chip's
	// colour: the badge's dark foreground is unreadable without its
	// background, and its padding would push the rule past the card edge.
	// lipgloss styles are value types so the Theme's badge is unaffected.
	word := sevStyle.Foreground(sevStyle.GetBackground()).
		Background(lipgloss.NoColor{}).
		UnsetPadding()
	return word.Render(sevTok) + t.CardBorder.Render(rest)
}

// cardLine writes one card body row padded to innerWidth runes
// between the side rules.
func cardLine(b *strings.Builder, t style.Theme, innerWidth int, content string) {
	visible := visibleRuneCount(content)
	if visible > innerWidth {
		// Callers wrap first; defensive truncate keeps column alignment.
		content = truncate(content, innerWidth)
		visible = innerWidth
	}
	pad := strings.Repeat(" ", innerWidth-visible)
	fmt.Fprintf(b, "%s%s %s%s %s\n",
		contentIndent,
		t.CardBorder.Render("│"),
		content,
		pad,
		t.CardBorder.Render("│"),
	)
}

// formatSignalLine renders a Signal as a one-liner sized to fit a
// card body row. Width-aware columns so stacked cards align.
func formatSignalLine(t style.Theme, s rules.Signal) string {
	bar := t.SignalBar(s.Have, s.Want, signalBarWidth)
	label := s.Label
	if label == "" {
		label = "ratio"
	}
	note := ""
	if s.Note != "" {
		note = "   " + t.Muted.Render(s.Note)
	}
	return fmt.Sprintf("%s %s %s %s%s",
		t.Muted.Render(padRight(label, 8)),
		t.Detail.Render(padRight(s.HaveDisplay, 6)),
		bar,
		t.Detail.Render(s.WantDisplay),
		note,
	)
}

// padRight pads s to width runes; never truncates.
func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(r))
}

// visibleRuneCount counts terminal cells in s, skipping ANSI/OSC
// escapes. One cell per rune (no wide-CJK in our copy).
func visibleRuneCount(s string) int {
	n := 0
	r := []rune(s)
	for i := 0; i < len(r); {
		if r[i] != 0x1b {
			n++
			i++
			continue
		}
		// CSI: ESC [ … <letter in @..~>
		if i+1 < len(r) && r[i+1] == '[' {
			j := i + 2
			for j < len(r) && (r[j] < '@' || r[j] > '~') {
				j++
			}
			if j < len(r) {
				j++
			}
			i = j
			continue
		}
		// OSC: ESC ] … (ESC \ | BEL)
		if i+1 < len(r) && r[i+1] == ']' {
			j := i + 2
			for j < len(r) {
				if r[j] == 0x07 {
					j++
					break
				}
				if r[j] == 0x1b && j+1 < len(r) && r[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
			continue
		}
		i += 2
	}
	return n
}

func writeSecuritySection(b *strings.Builder, t style.Theme, width int, findings []rules.Finding) {
	label := fmt.Sprintf("%s  (bonus, %d)", bonusSectionName, len(findings))
	b.WriteString("\n")
	fmt.Fprintf(b, "%s\n", t.SectionRule(label, width, t.SectionBonus))
	fmt.Fprintf(b, "%s%s\n\n", contentIndent,
		t.SectionSubtle.Render("Spotted while parsing your chart. Cost is the headline; this is a bonus."),
	)

	maxWorkload := 0
	for _, f := range findings {
		if n := len([]rune(f.Workload)); n > maxWorkload {
			maxWorkload = n
		}
	}
	if maxWorkload > 24 {
		maxWorkload = 24
	}

	for _, f := range findings {
		writeSecurityFinding(b, t, f, maxWorkload)
	}
	b.WriteString("\n")
	fmt.Fprintf(b, "%s%s\n\n", contentIndent,
		t.Muted.Render("Run `optiqor audit` to focus only on these findings."),
	)
}

func writeSecurityFinding(b *strings.Builder, t style.Theme, f rules.Finding, workloadColWidth int) {
	wl := truncate(f.Workload, workloadColWidth)
	wlPadded := wl + strings.Repeat(" ", workloadColWidth-len([]rune(wl)))
	title := f.Title
	if title == "" && f.DetectorID != "" {
		title = f.DetectorID
	}
	fmt.Fprintf(b, "%s%s  %s   %s   %s\n",
		contentIndent,
		t.SeverityBadge(string(f.Severity)),
		t.Workload.Render(wlPadded),
		t.ConfidenceGlyph(string(f.Confidence)),
		t.Detail.Render(title),
	)
}

func writeFooter(b *strings.Builder, t style.Theme, width int, totalCents int64, opts Options) {
	fmt.Fprintf(b, "%s\n", t.DividerLine(width))
	if totalCents > 0 {
		fmt.Fprintf(b, "%s%s %s   %s\n", contentIndent,
			t.Muted.Render("estimated monthly savings:"),
			t.BigSavings.Render("$"+formatCents(totalCents)+"/mo"),
			t.Muted.Render("(±40%)"),
		)
	}
	// Accuracy disclosure is mandatory and exact (CLAUDE.md hard rule).
	// Roast adds a quip BELOW; it never replaces.
	fmt.Fprintf(b, "%s%s\n", contentIndent, t.Disclosure.Render(AccuracyDisclosure))
	linkLabel := t.CallToLink.Render("optiqor.dev/get")
	fmt.Fprintf(b, "%s%s %s\n", contentIndent,
		t.Muted.Render("→ install the agent for exact numbers:"),
		t.Hyperlink(linkLabel, GetURL),
	)
	if opts.Roast && opts.RoastFooter != "" {
		fmt.Fprintf(b, "%s%s\n", contentIndent, t.Tagline.Render(opts.RoastFooter))
	}
}

// JSON writes the report as machine-readable JSON. Always includes
// the accuracy disclosure; never coloured. Findings are grouped by
// category so consumers don't have to replicate the split.
func JSON(w io.Writer, r Report) error {
	cost, security := splitByCategory(r.Findings)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		AccuracyDisclosure string          `json:"accuracy_disclosure"`
		Source             string          `json:"source"`
		Workloads          int             `json:"workloads_analyzed"`
		Findings           []rules.Finding `json:"findings"`
		Cost               []rules.Finding `json:"cost_findings"`
		Security           []rules.Finding `json:"security_findings_bonus"`
		MonthlySavingsUSD  float64         `json:"monthly_savings_usd"`
		AnnualSavingsUSD   float64         `json:"annual_savings_usd"`
	}{
		AccuracyDisclosure: AccuracyDisclosure,
		Source:             r.Source,
		Workloads:          r.Workloads,
		Findings:           r.Findings,
		Cost:               cost,
		Security:           security,
		MonthlySavingsUSD:  float64(r.MonthlySavingsUSDCents()) / 100.0,
		AnnualSavingsUSD:   float64(r.MonthlySavingsUSDCents()*monthsPerYear) / 100.0,
	})
}

func formatCents(c int64) string {
	dollars := c / 100
	cents := c % 100
	if cents == 0 {
		return fmt.Sprintf("%d", dollars)
	}
	return fmt.Sprintf("%d.%02d", dollars, cents)
}

func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, pluralForm)
}

// wrap breaks s into lines no wider than width runes. Naive word
// wrap; finding details are sentences, not paragraphs.
func wrap(s string, width int) []string {
	if width <= 0 || len([]rune(s)) <= width {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	words := strings.Fields(s)
	var lines []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len() == 0 {
			cur.WriteString(w)
			continue
		}
		if cur.Len()+1+len(w) > width {
			lines = append(lines, cur.String())
			cur.Reset()
			cur.WriteString(w)
			continue
		}
		cur.WriteString(" ")
		cur.WriteString(w)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}

// truncate clamps s to at most width runes, suffixing "…" on cut.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
}
