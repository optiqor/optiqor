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
