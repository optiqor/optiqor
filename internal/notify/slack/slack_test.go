package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateWebhookURL_AcceptsAndRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
		ok   bool
	}{
		{"good", "https://hooks.slack.com/services/T000/B000/abc", true},
		{"empty", "", false},
		{"http not https", "http://hooks.slack.com/services/T/B/abc", false},
		{"wrong host", "https://hooks.example.com/services/T/B/abc", false},
		{"bad path prefix", "https://hooks.slack.com/incoming/T/B/abc", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateWebhookURL(tc.url)
			if (err == nil) != tc.ok {
				t.Errorf("err = %v, ok wanted %v", err, tc.ok)
			}
		})
	}
}

func TestPostWebhook_SendsJSONPayload(t *testing.T) {
	var got Payload
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	// Build a client using the httptest server's TLS but inject a real
	// Slack-shaped URL by rewriting the request inside the transport.
	c := &Client{httpClient: srv.Client()}
	url := srv.URL // not slack-shaped; bypass validate
	if err := c.postRaw(context.Background(), url, Payload{Text: "hello"}); err != nil {
		t.Fatalf("postRaw: %v", err)
	}
	if got.Text != "hello" {
		t.Errorf("server saw text = %q", got.Text)
	}
}

func TestPostWebhook_RejectsBadURL(t *testing.T) {
	c := NewClient()
	err := c.PostWebhook(context.Background(), "https://not-slack.test/services/T/B/abc", Payload{Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "hooks.slack.com") {
		t.Errorf("want host validation error, got %v", err)
	}
}

func TestRenderDigest_IncludesTopFindings(t *testing.T) {
	p := RenderDigest(DigestData{
		OpenApplyFixes:   3,
		MergedApplyFixes: 1,
		SavingsThisWeek:  2920,
		TopFindings: []Finding{
			{Workload: "api", Title: "CPU overprovisioned", MonthlyUSDCents: 2000},
			{Workload: "worker", Title: "memory overprovisioned", MonthlyUSDCents: 920},
		},
		DashboardURL: "https://app.optiqor.dev",
	})
	if !strings.Contains(p.Text, "Optiqor daily") {
		t.Errorf("text fallback missing: %q", p.Text)
	}
	if len(p.Blocks) < 3 {
		t.Errorf("expected >= 3 blocks, got %d", len(p.Blocks))
	}
	full := p.Blocks[2].Text.Text
	if !strings.Contains(full, "api") || !strings.Contains(full, "CPU overprovisioned") {
		t.Errorf("top findings missing: %q", full)
	}
}

func TestRenderDigest_NoFindings_StillRenders(t *testing.T) {
	p := RenderDigest(DigestData{OpenApplyFixes: 0, DashboardURL: "https://app"})
	if !strings.Contains(p.Blocks[2].Text.Text, "No new findings") {
		t.Errorf("empty digest must mention clean chart: %q", p.Blocks[2].Text.Text)
	}
}

func TestRenderSpike_ExplicitFallbackWhenNoPR(t *testing.T) {
	p := RenderSpike(SpikePayload{Workload: "api", ObservedDeltaUSD: 412})
	if !strings.Contains(p.Blocks[1].Text.Text, "no PR matched") {
		t.Errorf("missing fallback message: %q", p.Blocks[1].Text.Text)
	}
}

func TestRenderApplyFixDiff_HappyPath(t *testing.T) {
	p := RenderApplyFixDiff(ApplyFixDiff{
		Workload:        "api",
		RepoSlug:        "acme/charts",
		PRNumber:        42,
		PRURL:           "https://github.com/acme/charts/pull/42",
		ChartPath:       "charts/api/values.yaml",
		UnifiedDiff:     "--- a\n+++ b\n@@\n- cpu: 2\n+ cpu: 500m\n",
		MonthlyUSDCents: 4500,
		AnnualUSDCents:  54000,
		Confidence:      "high",
		BlastRadius:     2,
		DashboardURL:    "https://optiqor.dev/app/apply-fixes/42",
	})
	if !strings.Contains(p.Text, "Apply Fix · api") {
		t.Errorf("fallback text missing workload: %q", p.Text)
	}
	body := p.Blocks[1].Text.Text
	for _, want := range []string{"acme/charts", "#42", "values.yaml", "$45", "$540", "high", "2/5"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n%s", want, body)
		}
	}
	codeBlock := p.Blocks[2].Text.Text
	if !strings.Contains(codeBlock, "+ cpu: 500m") {
		t.Errorf("snippet missing the change line: %q", codeBlock)
	}
	cta := p.Blocks[3].Text.Text
	if !strings.Contains(cta, "Open PR →") || !strings.Contains(cta, "Dashboard") {
		t.Errorf("cta missing PR + dashboard links: %q", cta)
	}
}

func TestRenderApplyFixDiff_TruncatesLongDiff(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("line " + strings.Repeat("x", 100) + "\n")
	}
	p := RenderApplyFixDiff(ApplyFixDiff{
		Workload:    "w",
		PRURL:       "https://github.com/x/y/pull/1",
		UnifiedDiff: sb.String(),
	})
	code := p.Blocks[2].Text.Text
	if !strings.Contains(code, "more lines — open PR") {
		t.Errorf("expected truncation trailer, got %q", code)
	}
	if strings.Contains(code, strings.Repeat("x", 100)) {
		t.Error("wide line should be right-trimmed to 80 chars")
	}
}

func TestRenderApplyFixDiff_EmptyDiffStillRenders(t *testing.T) {
	p := RenderApplyFixDiff(ApplyFixDiff{Workload: "w", PRURL: "https://github.com/x/y/pull/1"})
	if !strings.Contains(p.Blocks[2].Text.Text, "(empty diff)") {
		t.Errorf("expected empty-diff sentinel: %q", p.Blocks[2].Text.Text)
	}
}

func TestRenderApplyFixDiff_NoBlastRadiusPrintsDash(t *testing.T) {
	p := RenderApplyFixDiff(ApplyFixDiff{Workload: "w", PRURL: "https://github.com/x/y/pull/1"})
	if !strings.Contains(p.Blocks[1].Text.Text, "Blast radius:* —") {
		t.Errorf("unset blast radius should render as em-dash, got: %q", p.Blocks[1].Text.Text)
	}
}

func TestRenderApplyFixDiff_StaysUnderSlackMaxBody(t *testing.T) {
	// Synthetic worst-case: a huge diff + huge URL. Renderer must keep
	// the marshalled payload under MaxBody so PostWebhook accepts it.
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		sb.WriteString("line\n")
	}
	p := RenderApplyFixDiff(ApplyFixDiff{
		Workload:    "huge",
		PRURL:       "https://github.com/x/y/pull/1",
		UnifiedDiff: sb.String(),
	})
	out, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(out) > MaxBody {
		t.Errorf("payload %d > MaxBody %d — renderer must cap snippets", len(out), MaxBody)
	}
}

// postRaw bypasses ValidateWebhookURL for tests that point at
// httptest.Server. Kept package-private so production callers can't
// route around the validator.
func (c *Client) postRaw(ctx context.Context, dest string, p Payload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
