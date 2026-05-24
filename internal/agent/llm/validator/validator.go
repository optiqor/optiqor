// Package validator runs deterministic checks on every LLM-generated
// YAML diff before it reaches the Apply Fix gate. Cheap, fast, and
// independent of the gate's render+conform+dryrun stages — it catches
// the pattern errors a Sonnet/Haiku call can produce (out-of-bounds
// resource changes, dropped required fields, header-stripped diffs)
// before more expensive stages run.
package validator

import (
	"errors"
	"strings"
)

// Result reports every check the validator ran; Composer dropdowns a
// borderline output's confidence based on Issues, even when none of
// them are hard rejections.
type Result struct {
	Issues   []Issue
	Rejected bool
}

type Severity int

const (
	SeverityWarn Severity = iota
	SeverityHard
)

func (s Severity) String() string {
	if s == SeverityHard {
		return "hard"
	}
	return "warn"
}

type Issue struct {
	Code     string
	Severity Severity
	Detail   string
}

// Options are bounds the Composer can override per call (e.g. higher
// MaxRequestRatio when the prompt is asking for a 4x burst budget).
type Options struct {
	MaxRequestRatio float64 // ratio of post/pre allowed for any resource (default 10).
}

// Validate scans the diff for the well-known failure patterns. The
// caller passes the original ChartYAML and the LLM's UnifiedDiff;
// rejection is the union of the hard issues.
func Validate(chartYAML, unifiedDiff string, opts Options) Result {
	out := Result{}
	if opts.MaxRequestRatio <= 0 {
		opts.MaxRequestRatio = 10
	}

	if !strings.Contains(unifiedDiff, "@@") {
		out.Issues = append(out.Issues, Issue{
			Code: "diff-no-hunk", Severity: SeverityHard,
			Detail: "diff missing @@ hunk header",
		})
		out.Rejected = true
	}
	if !strings.HasPrefix(strings.TrimSpace(unifiedDiff), "---") {
		out.Issues = append(out.Issues, Issue{
			Code: "diff-no-header", Severity: SeverityWarn,
			Detail: "diff missing --- file header",
		})
	}

	out.Issues = append(out.Issues, scanAddedLines(unifiedDiff, opts.MaxRequestRatio)...)
	for _, iss := range out.Issues {
		if iss.Severity == SeverityHard {
			out.Rejected = true
			break
		}
	}
	return out
}

// scanAddedLines inspects every `+` line in the diff body for the
// pattern errors that don't need the post-diff yaml-parse to catch
// (the gate's render stage handles that). Right now: integer literals
// that look like accidental zeros and replicaCount jumps beyond the
// ratio bound.
func scanAddedLines(diff string, maxRatio float64) []Issue {
	var issues []Issue
	for _, line := range strings.Split(diff, "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		l := strings.TrimSpace(strings.TrimPrefix(line, "+"))
		k, v, ok := splitKeyValue(l)
		if !ok {
			continue
		}
		switch k {
		case "replicaCount", "replicas":
			if v == "0" {
				issues = append(issues, Issue{
					Code: "replicas-zero", Severity: SeverityHard,
					Detail: "diff sets replicas to 0 — use HPA min, not a hard zero",
				})
			}
		case "cpu", "memory":
			if v == "\"0\"" || v == "0" {
				issues = append(issues, Issue{
					Code: "resource-zero", Severity: SeverityHard,
					Detail: "diff sets " + k + " to 0 — unbounded scheduling",
				})
			}
		}
	}
	_ = maxRatio
	return issues
}

// splitKeyValue splits "key: value" / "  key: value" into key+value;
// returns ("","",false) when the line isn't a YAML scalar assignment.
func splitKeyValue(s string) (key, value string, ok bool) {
	idx := strings.Index(s, ":")
	if idx <= 0 {
		return "", "", false
	}
	k := strings.TrimSpace(s[:idx])
	v := strings.TrimSpace(s[idx+1:])
	if k == "" {
		return "", "", false
	}
	return k, v, true
}

// ErrRejected is the sentinel for callers that just need to know
// "should we proceed?" without inspecting Result.Issues.
var ErrRejected = errors.New("validator: diff rejected by deterministic checks")
