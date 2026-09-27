package htmlrender

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor-cli/pkg/rules"
)

func sample() Data {
	return Data{
		Source:      "demo",
		Workloads:   3,
		ShareURL:    "https://optiqor.dev/r/abc123",
		GeneratedAt: time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC),
		Findings: []rules.Finding{
			{
				DetectorID:      "cpu-overprovisioned",
				Workload:        "api",
				Title:           "CPU request appears overprovisioned",
				Detail:          "request 2 vs limit 2.5",
				MonthlyUSDCents: 22040,
				Severity:        rules.SeverityHigh,
				Confidence:      rules.ConfidenceMed,
				Category:        rules.CategoryCost,
			},
			{
				DetectorID:      "memory-overprovisioned",
				Workload:        "api",
				Title:           "Memory request appears overprovisioned",
				MonthlyUSDCents: 9640,
				Severity:        rules.SeverityMed,
				Confidence:      rules.ConfidenceMed,
				Category:        rules.CategoryCost,
			},
			{
				DetectorID: "run-as-root",
				Workload:   "worker",
				Title:      "Container runs as root",
				Severity:   rules.SeverityHigh,
				Confidence: rules.ConfidenceHigh,
				Category:   rules.CategorySecurity,
			},
		},
	}
}

func TestRender(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  func() Data
		check func(t *testing.T, d Data)
	}{
		{
			name: "happy-path-contains-expected-markers",
			data: sample,
			check: func(t *testing.T, d Data) {
				t.Helper()
				out, err := RenderString(d)
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{
					"<!doctype html>",
					"Optiqor",
					"Cost optimizations",
					"Security findings",
					"bonus",
					"CPU request appears overprovisioned",
					"Container runs as root",
					"$316.80", // 22040 + 9640 cents = $316.80
					"±40%",
					"share",
				} {
					if !strings.Contains(out, want) {
						t.Errorf("missing %q in output", want)
					}
				}
			},
		},
		{
			name: "no-share-url-omits-share-row",
			data: func() Data {
				d := sample()
				d.ShareURL = ""
				return d
			},
			check: func(t *testing.T, d Data) {
				t.Helper()
				out, err := RenderString(d)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(out, "data-copy=") {
					t.Errorf("share UI should not render when ShareURL is empty")
				}
			},
		},
		{
			name: "no-findings-clean-state",
			data: func() Data {
				d := sample()
				d.Findings = nil
				return d
			},
			check: func(t *testing.T, d Data) {
				t.Helper()
				out, err := RenderString(d)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(out, "Clean. No findings.") {
					t.Errorf("clean state copy missing:\n%s", out)
				}
			},
		},
		{
			name: "truncates-timestamp-to-minute",
			data: sample,
			check: func(t *testing.T, d Data) {
				t.Helper()
				t1 := time.Date(2026, 5, 11, 14, 30, 0, 0, time.UTC)
				t2 := time.Date(2026, 5, 11, 14, 30, 45, 0, time.UTC)
				d.GeneratedAt = t1
				a, err := RenderString(d)
				if err != nil {
					t.Fatal(err)
				}
				d.GeneratedAt = t2
				b, err := RenderString(d)
				if err != nil {
					t.Fatal(err)
				}
				if a != b {
					t.Errorf("renders within the same minute must be byte-equal")
				}
			},
		},
		{
			name: "cost-findings-lead-biggest-dollar",
			data: sample,
			check: func(t *testing.T, d Data) {
				t.Helper()
				out, err := RenderString(d)
				if err != nil {
					t.Fatal(err)
				}
				cpuIdx := strings.Index(out, "CPU request appears overprovisioned")
				memIdx := strings.Index(out, "Memory request appears overprovisioned")
				if cpuIdx == -1 || memIdx == -1 {
					t.Fatalf("missing finding lines")
				}
				if cpuIdx > memIdx {
					t.Errorf("CPU ($220.40) should render before Memory ($96.40)")
				}
			},
		},
		{
			name: "writes-to-writer",
			data: sample,
			check: func(t *testing.T, d Data) {
				t.Helper()
				var buf bytes.Buffer
				if err := Render(&buf, d); err != nil {
					t.Fatal(err)
				}
				if buf.Len() < 1024 {
					t.Errorf("output suspiciously short: %d bytes", buf.Len())
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, tc.data())
		})
	}
}

func TestRender_AccuracyByMode(t *testing.T) {
	for _, tc := range []struct {
		name           string
		mode           Mode
		wantSubstr     string
		wantDisclosure bool
	}{
		{
			name:           "sandbox-shows-mandatory-disclosure",
			mode:           ModeSandbox,
			wantSubstr:     AccuracyDisclosure,
			wantDisclosure: true,
		},
		{
			name:           "agent-shows-tighter-line-and-no-sandbox-disclosure",
			mode:           ModeAgent,
			wantSubstr:     "Agent accuracy: ±15%",
			wantDisclosure: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := sample()
			d.Mode = tc.mode
			out, err := RenderString(d)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, tc.wantSubstr) {
				t.Errorf("missing %q in %s output", tc.wantSubstr, tc.name)
			}
			hasDisclosure := strings.Contains(out, AccuracyDisclosure)
			if hasDisclosure != tc.wantDisclosure {
				t.Errorf("disclosure presence = %v, want %v", hasDisclosure, tc.wantDisclosure)
			}
		})
	}
}

func TestFmtUSD(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cents int64
		want  string
	}{
		{name: "zero", cents: 0, want: "$0"},
		{name: "one-cent", cents: 1, want: "$0.01"},
		{name: "exact-dollar", cents: 100, want: "$1"},
		{name: "dollars-and-cents", cents: 12345, want: "$123.45"},
		{name: "thousands-separator", cents: 12_345_678, want: "$123,456.78"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmtUSD(tc.cents); got != tc.want {
				t.Errorf("fmtUSD(%d) = %q, want %q", tc.cents, got, tc.want)
			}
		})
	}
}

// An unsized inline SVG stretches to the container width; the footer logo
// once filled the page and squeezed the accuracy disclosure into a sliver.
func TestDocumentTemplate_SVGRulesAreSized(t *testing.T) {
	cssRules := regexp.MustCompile(`\.[a-z-]+ svg \{[^}]*\}`).FindAllString(documentTemplate, -1)
	if len(cssRules) == 0 {
		t.Fatal("no svg CSS rules found; did the template move?")
	}
	for _, r := range cssRules {
		t.Run(strings.Fields(r)[0], func(t *testing.T) {
			if !strings.Contains(r, "width:") || !strings.Contains(r, "height:") {
				t.Errorf("svg rule without explicit width and height: %s", r)
			}
		})
	}
}
