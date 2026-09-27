package gate

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// RenderValidator applies the unified diff to ChartYAML and YAML-parses
// the result. Catches LLM outputs that would corrupt the chart before
// the diff hits a real `helm template` invocation (the agent-side
// dryrun stage handles that in Phase 5).
type RenderValidator struct{}

func (RenderValidator) Stage() Stage { return StageTemplate }

func (RenderValidator) Validate(_ context.Context, _ tenancy.Context, c Candidate) StageResult {
	if strings.TrimSpace(c.UnifiedDiff) == "" {
		return failed(StageTemplate, "empty diff", errNoDiff)
	}
	if strings.TrimSpace(c.ChartYAML) == "" {
		return failed(StageTemplate, "empty chart yaml", errNoChart)
	}
	patched, err := applyUnifiedDiff(c.ChartYAML, c.UnifiedDiff)
	if err != nil {
		return failed(StageTemplate, "diff did not apply", err)
	}
	if err := yaml.Unmarshal([]byte(patched), new(any)); err != nil {
		return failed(StageTemplate, "post-diff yaml invalid", err)
	}
	return StageResult{Stage: StageTemplate, Status: StatusPassed}
}

var (
	errNoDiff        = errors.New("gate/render: empty unified diff")
	errNoChart       = errors.New("gate/render: empty chart yaml")
	errMalformedHunk = errors.New("gate/render: malformed hunk header")
	errContextMiss   = errors.New("gate/render: hunk context line not found in source")
)

func failed(s Stage, detail string, err error) StageResult {
	return StageResult{Stage: s, Status: StatusFailed, Detail: detail, Err: err}
}

type hunk struct {
	oldStart, oldLen int
	operations       []rune
	body             []string
}

// applyUnifiedDiff handles single-file diffs with one or more
// `@@ -a,b +c,d @@` hunks. `---` / `+++` headers and "\ No newline"
// markers are ignored.
func applyUnifiedDiff(src, diff string) (string, error) {
	srcLines := strings.Split(src, "\n")

	var hunks []hunk
	var cur *hunk
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			continue
		}
		if strings.HasPrefix(line, "@@") {
			h, err := parseHunkHeader(line)
			if err != nil {
				return "", err
			}
			hunks = append(hunks, h)
			cur = &hunks[len(hunks)-1]
			continue
		}
		if cur == nil {
			continue
		}
		if line == "" {
			cur.operations = append(cur.operations, ' ')
			cur.body = append(cur.body, "")
			continue
		}
		op, payload := line[0], line[1:]
		switch op {
		case ' ', '-', '+':
			cur.operations = append(cur.operations, rune(op))
			cur.body = append(cur.body, payload)
		case '\\':
			continue
		default:
			cur.operations = append(cur.operations, ' ')
			cur.body = append(cur.body, line)
		}
	}
	if len(hunks) == 0 {
		return "", errors.New("gate/render: no hunks in diff")
	}

	out := append([]string(nil), srcLines...)
	cursor := 0
	for _, h := range hunks {
		var oldBlock, newBlock []string
		for i, op := range h.operations {
			switch op {
			case ' ':
				oldBlock = append(oldBlock, h.body[i])
				newBlock = append(newBlock, h.body[i])
			case '-':
				oldBlock = append(oldBlock, h.body[i])
			case '+':
				newBlock = append(newBlock, h.body[i])
			}
		}
		idx, err := findBlock(out, oldBlock, cursor, h.oldStart-1)
		if err != nil {
			return "", err
		}
		out = append(out[:idx], append(append([]string{}, newBlock...), out[idx+len(oldBlock):]...)...)
		cursor = idx + len(newBlock)
	}
	return strings.Join(out, "\n"), nil
}

func parseHunkHeader(line string) (hunk, error) {
	var h hunk
	rest := strings.TrimPrefix(line, "@@")
	rest = strings.TrimSpace(rest)
	parts := strings.SplitN(rest, " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "-") {
		return h, errMalformedHunk
	}
	startStr, lenStr := splitNum(strings.TrimPrefix(parts[0], "-"))
	start, err := strconv.Atoi(startStr)
	if err != nil {
		return h, fmt.Errorf("%w: %s", errMalformedHunk, line)
	}
	h.oldStart = start
	if lenStr != "" {
		l, err := strconv.Atoi(lenStr)
		if err != nil {
			return h, fmt.Errorf("%w: %s", errMalformedHunk, line)
		}
		h.oldLen = l
	} else {
		h.oldLen = 1
	}
	return h, nil
}

func splitNum(s string) (start, length string) {
	if i := strings.IndexByte(s, ','); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func findBlock(src, want []string, cursor, hint int) (int, error) {
	if len(want) == 0 {
		if hint >= 0 && hint <= len(src) {
			return hint, nil
		}
		return cursor, nil
	}
	tryAt := func(i int) bool {
		if i < 0 || i+len(want) > len(src) {
			return false
		}
		for k := range want {
			if src[i+k] != want[k] {
				return false
			}
		}
		return true
	}
	if tryAt(hint) {
		return hint, nil
	}
	for i := cursor; i+len(want) <= len(src); i++ {
		if tryAt(i) {
			return i, nil
		}
	}
	return -1, errContextMiss
}
