package gate

import (
	"context"
	"errors"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// PostValidator runs the deterministic checks that ride on top of a
// successfully-rendered diff. Catches the LLM error patterns the
// composer's prompt instructs against: removed labels, resource
// reductions beyond a safe bound, blank required fields.
type PostValidator struct {
	// MaxResourceReductionRatio caps how aggressively a CPU/memory
	// request or limit may shrink in a single Apply Fix. 0.5 means a
	// 50% cut is the floor; tighter is acceptable.
	MaxResourceReductionRatio float64
}

func (PostValidator) Stage() Stage { return StagePost }

func (v PostValidator) Validate(_ context.Context, _ tenancy.Context, c Candidate) StageResult {
	if strings.TrimSpace(c.ChartYAML) == "" || strings.TrimSpace(c.UnifiedDiff) == "" {
		return failed(StagePost, "empty input", errors.New("gate/post: empty chart or diff"))
	}
	before, err := decodeYAML(c.ChartYAML)
	if err != nil {
		return failed(StagePost, "pre-diff yaml invalid", err)
	}
	patched, err := applyUnifiedDiff(c.ChartYAML, c.UnifiedDiff)
	if err != nil {
		return failed(StagePost, "diff did not apply", err)
	}
	after, err := decodeYAML(patched)
	if err != nil {
		return failed(StagePost, "post-diff yaml invalid", err)
	}
	if err := checkLabelsPreserved(before, after); err != nil {
		return failed(StagePost, "labels removed", err)
	}
	ratioCap := v.MaxResourceReductionRatio
	if ratioCap <= 0 {
		ratioCap = 0.5
	}
	if err := checkResourceBounds(before, after, ratioCap); err != nil {
		return failed(StagePost, "resource cut exceeds safety floor", err)
	}
	return StageResult{Stage: StagePost, Status: StatusPassed}
}

func decodeYAML(s string) (map[string]any, error) {
	out := map[string]any{}
	if err := yaml.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// checkLabelsPreserved walks the chart looking for `labels:` blocks.
// Every key present before must survive in the after-state; removing
// a label silently breaks Service selectors and HPA target refs.
func checkLabelsPreserved(before, after map[string]any) error {
	var beforeLabels, afterLabels []string
	walkLabels(before, &beforeLabels)
	walkLabels(after, &afterLabels)
	seen := map[string]bool{}
	for _, k := range afterLabels {
		seen[k] = true
	}
	for _, k := range beforeLabels {
		if !seen[k] {
			return errors.New("gate/post: label removed by diff: " + k)
		}
	}
	return nil
}

func walkLabels(node any, out *[]string) {
	switch n := node.(type) {
	case map[string]any:
		if labels, ok := n["labels"].(map[string]any); ok {
			for k := range labels {
				*out = append(*out, k)
			}
		}
		for _, v := range n {
			walkLabels(v, out)
		}
	case []any:
		for _, v := range n {
			walkLabels(v, out)
		}
	}
}

// checkResourceBounds compares CPU/memory requests + limits before vs.
// after; rejects diffs that shrink any of them by more than the cap.
func checkResourceBounds(before, after map[string]any, ratioCap float64) error {
	type tuple struct{ b, a float64 }
	beforeRes := collectResources(before)
	afterRes := collectResources(after)
	for path, b := range beforeRes {
		a, ok := afterRes[path]
		if !ok {
			return errors.New("gate/post: resource removed: " + path)
		}
		if b > 0 && a < b*(1-ratioCap) {
			return errors.New("gate/post: " + path + " cut beyond safety floor")
		}
		_ = tuple{b: b, a: a}
	}
	return nil
}

// collectResources flattens all CPU/memory request+limit pairs into
// path → quantity. Quantities are normalised to a numeric scale
// suitable for ratio comparison; missing keys are skipped.
func collectResources(root map[string]any) map[string]float64 {
	out := map[string]float64{}
	collectResWalk(root, "", out)
	return out
}

func collectResWalk(node any, path string, out map[string]float64) {
	switch n := node.(type) {
	case map[string]any:
		if res, ok := n["resources"].(map[string]any); ok {
			for kind, sub := range res {
				if m, ok := sub.(map[string]any); ok {
					for k, v := range m {
						if q := parseQuantity(v); q > 0 {
							out[path+".resources."+kind+"."+k] = q
						}
					}
				}
			}
		}
		for k, v := range n {
			if k == "resources" {
				continue
			}
			collectResWalk(v, path+"."+k, out)
		}
	case []any:
		for i, v := range n {
			collectResWalk(v, path+"["+itoa(i)+"]", out)
		}
	}
}

// parseQuantity handles "100m", "2", "256Mi", "1Gi"; falls back to 0
// on parse failure so unknown shapes don't trigger false positives.
func parseQuantity(v any) float64 {
	s, ok := v.(string)
	if !ok {
		if i, ok := v.(int); ok {
			return float64(i)
		}
		if f, ok := v.(float64); ok {
			return f
		}
		return 0
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Strip trailing unit. Order matters: longer suffixes first.
	for _, u := range []struct {
		suffix string
		mult   float64
	}{
		{"Ei", 1 << 60}, {"Pi", 1 << 50}, {"Ti", 1 << 40}, {"Gi", 1 << 30}, {"Mi", 1 << 20}, {"Ki", 1 << 10},
		{"E", 1e18}, {"P", 1e15}, {"T", 1e12}, {"G", 1e9}, {"M", 1e6}, {"K", 1e3},
		{"m", 0.001},
	} {
		if strings.HasSuffix(s, u.suffix) {
			n := parseFloat(strings.TrimSuffix(s, u.suffix))
			return n * u.mult
		}
	}
	return parseFloat(s)
}

func parseFloat(s string) float64 {
	var out float64
	var frac float64 = 1
	var inFrac bool
	for _, r := range s {
		switch {
		case r == '.':
			inFrac = true
		case r >= '0' && r <= '9':
			d := float64(r - '0')
			if inFrac {
				frac *= 10
				out += d / frac
			} else {
				out = out*10 + d
			}
		default:
			return out
		}
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [16]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
