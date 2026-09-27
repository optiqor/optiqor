package gate

import (
	"context"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func TestPostValidator_Validate(t *testing.T) {
	chart := strings.Join([]string{
		"api:",
		"  labels:",
		"    app: api",
		"    tier: web",
		"  resources:",
		"    requests:",
		"      cpu: \"2\"",
		"      memory: 4Gi",
		"    limits:",
		"      cpu: \"4\"",
		"      memory: 8Gi",
		"",
	}, "\n")

	safeReduction := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -6,3 +6,3 @@",
		"     requests:",
		"-      cpu: \"2\"",
		"+      cpu: \"1500m\"",
		"       memory: 4Gi",
	}, "\n")

	unsafeReduction := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -6,3 +6,3 @@",
		"     requests:",
		"-      cpu: \"2\"",
		"+      cpu: \"100m\"",
		"       memory: 4Gi",
	}, "\n")

	labelRemoval := strings.Join([]string{
		"--- a/values.yaml",
		"+++ b/values.yaml",
		"@@ -2,3 +2,2 @@",
		"   labels:",
		"     app: api",
		"-    tier: web",
	}, "\n")

	ctx := context.Background()
	tnt := tenancy.Context{TenantID: "t-1"}
	v := PostValidator{MaxResourceReductionRatio: 0.5}

	for _, tc := range []struct {
		name     string
		cand     Candidate
		wantStat Status
		errSub   string
	}{
		{name: "safe reduction passes", cand: Candidate{ChartYAML: chart, UnifiedDiff: safeReduction}, wantStat: StatusPassed},
		{name: "75% cut fails", cand: Candidate{ChartYAML: chart, UnifiedDiff: unsafeReduction}, wantStat: StatusFailed, errSub: "safety floor"},
		{name: "label removal fails", cand: Candidate{ChartYAML: chart, UnifiedDiff: labelRemoval}, wantStat: StatusFailed, errSub: "labels removed"},
		{name: "empty inputs fail", cand: Candidate{}, wantStat: StatusFailed, errSub: "empty input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := v.Validate(ctx, tnt, tc.cand)
			if got.Status != tc.wantStat {
				t.Fatalf("status=%s want %s (detail=%q err=%v)", got.Status, tc.wantStat, got.Detail, got.Err)
			}
			if tc.errSub != "" && !strings.Contains(got.Detail, tc.errSub) {
				t.Errorf("detail %q missing %q", got.Detail, tc.errSub)
			}
		})
	}
}

func TestParseQuantity(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want float64
	}{
		{"2", 2},
		{"100m", 0.1},
		{"1500m", 1.5},
		{"256Mi", 256 * (1 << 20)},
		{"1Gi", 1 << 30},
		{2, 2},
		{1.5, 1.5},
		{"", 0},
		{"bogus", 0},
	} {
		got := parseQuantity(tc.in)
		if got != tc.want {
			t.Errorf("parseQuantity(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
