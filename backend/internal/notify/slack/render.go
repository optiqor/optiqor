package slack

import (
	"fmt"
	"strings"
	"time"
)

// DigestData is the daily digest's input. Kept small and pre-aggregated
// — the worker reads the dashboard endpoint shapes and projects them.
type DigestData struct {
	Tenant           string
	GeneratedAt      time.Time
	WindowDays       int
	OpenApplyFixes   int
	MergedApplyFixes int
	SavingsThisWeek  int64 // cents
	TopFindings      []Finding
	DashboardURL     string
}

// WeeklyData widens the digest with merge counts + receipts.
type WeeklyData struct {
	Tenant          string
	GeneratedAt     time.Time
	MergedThisWeek  int
	ReceiptsIssued  int
	SavingsRealised int64
	DashboardURL    string
}

// Finding is the slack-projected view; mirrors rules.Finding but stays
// independent so a rules schema change does not break Slack rendering.
type Finding struct {
	Workload        string
	Title           string
	MonthlyUSDCents int64
	Severity        string
}

// SpikePayload is the cost-spike alert. SpikeNotifier callers translate
// SpikeEvent into this shape; keeping that mapping out of this package
// preserves package isolation (slack/ never imports billing).
type SpikePayload struct {
	Workload         string
	ObservedDeltaUSD float64
	LikelyPRURL      string
	DashboardURL     string
}

// RenderDigest builds the daily digest payload. The text field is the
// fallback for clients without Block Kit; blocks render the rich post.
func RenderDigest(d DigestData) Payload {
	var top strings.Builder
	for i, f := range d.TopFindings {
		if i >= 3 {
			break
		}
		fmt.Fprintf(&top, "• *%s* — %s (save %s/mo)\n", f.Workload, f.Title, fmtUSD(f.MonthlyUSDCents))
	}
	if top.Len() == 0 {
		top.WriteString("• _No new findings in the last 24h — chart is clean._\n")
	}

	return Payload{
		Text: fmt.Sprintf("Optiqor daily — %d open Apply Fixes, %s saved this week", d.OpenApplyFixes, fmtUSD(d.SavingsThisWeek)),
		Blocks: []Block{
			{Type: "header", Text: &Text{Type: "plain_text", Text: "Optiqor — daily digest"}},
			{Type: "section", Text: &Text{Type: "mrkdwn", Text: fmt.Sprintf("*Open Apply Fixes:* %d   ·   *Merged (7d):* %d   ·   *Saved this week:* %s",
				d.OpenApplyFixes, d.MergedApplyFixes, fmtUSD(d.SavingsThisWeek))}},
			{Type: "section", Text: &Text{Type: "mrkdwn", Text: "*Top findings*\n" + top.String()}},
			{Type: "section", Text: &Text{Type: "mrkdwn", Text: fmt.Sprintf("<%s|Open dashboard →>", d.DashboardURL)}},
		},
	}
}

// RenderWeekly is the Monday-morning team report.
func RenderWeekly(d WeeklyData) Payload {
	return Payload{
		Text: fmt.Sprintf("Optiqor weekly — %d merged, %s realised", d.MergedThisWeek, fmtUSD(d.SavingsRealised)),
		Blocks: []Block{
			{Type: "header", Text: &Text{Type: "plain_text", Text: "Optiqor — weekly report"}},
			{Type: "section", Text: &Text{Type: "mrkdwn", Text: fmt.Sprintf(
				"*Apply Fixes merged this week:* %d\n*Receipts issued:* %d\n*Realised savings:* %s",
				d.MergedThisWeek, d.ReceiptsIssued, fmtUSD(d.SavingsRealised))}},
			{Type: "section", Text: &Text{Type: "mrkdwn", Text: fmt.Sprintf("<%s|Open dashboard →>", d.DashboardURL)}},
		},
	}
}

// ApplyFixDiff carries the per-PR data the Slack thread renders.
// Designed for mobile review: 3-line preview + savings + tap-through
// link to the PR. Reviewer doesn't need to open GitHub to triage.
type ApplyFixDiff struct {
	Workload         string
	RepoSlug         string // "owner/repo"
	PRNumber         int
	PRURL            string
	ChartPath        string
	UnifiedDiff      string
	MonthlyUSDCents  int64
	AnnualUSDCents   int64
	Confidence       string // "high" | "medium" | "low"
	BlastRadius      int    // 1..5; 0 unset
	GeneratedAt      time.Time
	DiffPreviewLines int // 0 falls through to defaultDiffPreviewLines
	DashboardURL     string
}

// defaultDiffPreviewLines bounds the inline snippet so a 2k-line YAML
// rewrite doesn't break Slack's 40 KiB envelope. Mobile reviewers see
// the change shape; the PRURL is the source of truth.
const defaultDiffPreviewLines = 20

// RenderApplyFixDiff turns one ApplyFixDiff into a thread-ready post.
// Layout: header + savings strip + diff snippet in a fenced code block
// + tap-through. Optimised for the iPhone Slack client's column width
// (~38 monospace chars); long diff lines are right-trimmed not wrapped
// because Slack wraps inside fenced blocks unpredictably.
func RenderApplyFixDiff(d ApplyFixDiff) Payload {
	lines := d.DiffPreviewLines
	if lines <= 0 {
		lines = defaultDiffPreviewLines
	}
	preview := truncateDiff(d.UnifiedDiff, lines)
	confidence := d.Confidence
	if confidence == "" {
		confidence = "unset"
	}
	blast := "—"
	if d.BlastRadius > 0 {
		blast = fmt.Sprintf("%d/5", d.BlastRadius)
	}

	header := fmt.Sprintf("Apply Fix · %s · save %s/mo", d.Workload, fmtUSD(d.MonthlyUSDCents))
	body := fmt.Sprintf(
		"*Repo:* `%s`#%d\n*Chart:* `%s`\n*Monthly:* %s   ·   *Annual:* %s\n*Confidence:* %s   ·   *Blast radius:* %s",
		d.RepoSlug, d.PRNumber, d.ChartPath,
		fmtUSD(d.MonthlyUSDCents), fmtUSD(d.AnnualUSDCents),
		confidence, blast,
	)
	cta := fmt.Sprintf("<%s|Open PR →>", d.PRURL)
	if d.DashboardURL != "" {
		cta += fmt.Sprintf("   ·   <%s|Dashboard>", d.DashboardURL)
	}

	return Payload{
		Text: header,
		Blocks: []Block{
			{Type: "header", Text: &Text{Type: "plain_text", Text: header}},
			{Type: "section", Text: &Text{Type: "mrkdwn", Text: body}},
			{Type: "section", Text: &Text{Type: "mrkdwn", Text: "```\n" + preview + "\n```"}},
			{Type: "section", Text: &Text{Type: "mrkdwn", Text: cta}},
		},
	}
}

// truncateDiff caps the snippet at maxLines, appending a "(+N lines)"
// trailer so the reviewer knows there is more out of frame. Lines past
// 80 chars get a single right-trim marker so wide YAML diffs don't blow
// out the mobile column.
func truncateDiff(diff string, maxLines int) string {
	if diff == "" {
		return "(empty diff)"
	}
	const maxWidth = 80
	lines := strings.Split(diff, "\n")
	var b strings.Builder
	limit := maxLines
	if len(lines) < limit {
		limit = len(lines)
	}
	for i := 0; i < limit; i++ {
		line := lines[i]
		if len(line) > maxWidth {
			line = line[:maxWidth-1] + "…"
		}
		b.WriteString(line)
		if i < limit-1 {
			b.WriteString("\n")
		}
	}
	if extra := len(lines) - limit; extra > 0 {
		fmt.Fprintf(&b, "\n(+%d more lines — open PR for the full diff)", extra)
	}
	return b.String()
}

// RenderSpike formats the cost-spike alert. Severity is implicit —
// any spike that hits this path already passed the workflow's
// detection threshold.
func RenderSpike(p SpikePayload) Payload {
	prLink := "_no PR matched_"
	if p.LikelyPRURL != "" {
		prLink = fmt.Sprintf("<%s|likely PR>", p.LikelyPRURL)
	}
	return Payload{
		Text: fmt.Sprintf("Optiqor cost spike — %s +$%.0f (24h)", p.Workload, p.ObservedDeltaUSD),
		Blocks: []Block{
			{Type: "header", Text: &Text{Type: "plain_text", Text: "Optiqor — cost spike"}},
			{Type: "section", Text: &Text{Type: "mrkdwn", Text: fmt.Sprintf(
				"*Workload:* `%s`\n*Observed delta (24h):* *$%.0f*\n*Likely cause:* %s",
				p.Workload, p.ObservedDeltaUSD, prLink)}},
			{Type: "section", Text: &Text{Type: "mrkdwn", Text: fmt.Sprintf("<%s|Open dashboard →>", p.DashboardURL)}},
		},
	}
}

func fmtUSD(cents int64) string {
	if cents <= 0 {
		return "$0"
	}
	dollars := cents / 100
	c := cents % 100
	if c == 0 {
		return fmt.Sprintf("$%d", dollars)
	}
	return fmt.Sprintf("$%d.%02d", dollars, c)
}
