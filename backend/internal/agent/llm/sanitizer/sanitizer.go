// Package sanitizer hardens LLM input from customer-controlled Helm
// values: strip comments, flag injection markers, cap field length,
// and wrap any remaining suspect text in <USER_DATA> boundaries the
// system prompt instructs the LLM never to trust. Layer 1 of the LLM
// defence stack; Layer 2 (output validation) reuses the Apply Fix
// render/conform/dryrun gate.
package sanitizer

import (
	"errors"
	"regexp"
	"strings"
)

// Defaults match production budgets; tunable per-tenant later.
const (
	DefaultMaxFieldLen = 8 * 1024 // 8 KiB per Helm value field
	DefaultMaxTotalLen = 256 * 1024
)

// Result.Suspicious=true should also be surfaced to the audit log and
// down-rank recommendation confidence one band per the LLM defense plan.
type Result struct {
	Output     string
	Suspicious bool
	Reasons    []string
	Truncated  bool
}

type Options struct {
	MaxFieldLen int
	MaxTotalLen int
	// WrapSuspicious defaults to true.
	WrapSuspicious *bool
}

// Sanitize is deterministic and side-effect free.
func Sanitize(input string, opts Options) (Result, error) {
	if opts.MaxFieldLen <= 0 {
		opts.MaxFieldLen = DefaultMaxFieldLen
	}
	if opts.MaxTotalLen <= 0 {
		opts.MaxTotalLen = DefaultMaxTotalLen
	}
	wrap := true
	if opts.WrapSuspicious != nil {
		wrap = *opts.WrapSuspicious
	}

	if len(input) > opts.MaxTotalLen {
		return Result{}, errors.New("sanitizer: input exceeds total length budget")
	}

	r := Result{}

	// Scan injections BEFORE stripping comments — markers hiding in
	// `#` comments are evidence of intent and should down-rank
	// confidence even when the output ends up clean.
	if reasons := scanInjections(input); len(reasons) > 0 {
		r.Suspicious = true
		r.Reasons = append(r.Reasons, reasons...)
	}

	stripped := stripComments(input)
	stripped, truncated := truncateLongFields(stripped, opts.MaxFieldLen)
	if truncated {
		r.Truncated = true
		r.Reasons = append(r.Reasons, "long-field truncated")
	}
	r.Output = stripped

	if r.Suspicious && wrap {
		r.Output = wrapUserData(r.Output)
	}

	return r, nil
}

// Patterns are intentionally lenient: real injection phrasing varies,
// false positives only down-rank confidence (never fail the analysis).
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(ignore|disregard|forget|override)\b[^\n]{0,60}\b(instructions?|prompts?|context|directives?|rules?)\b`),
	regexp.MustCompile(`(?i)\bnow do\b`),
	regexp.MustCompile(`(?i)\byou are now\b`),
	regexp.MustCompile(`(?im)^\s*system\s*:`),
	regexp.MustCompile(`(?im)^\s*assistant\s*:`),
	regexp.MustCompile(`(?im)<\|(im_start|im_end|system|user|assistant)\|>`),
	regexp.MustCompile(`(?i)\brepeat (the )?prompt\b`),
	regexp.MustCompile(`(?i)\bdo bad things\b`),
}

func scanInjections(s string) []string {
	var reasons []string
	for _, re := range injectionPatterns {
		if re.MatchString(s) {
			reasons = append(reasons, "injection-marker: "+re.String())
		}
	}
	return reasons
}

// stripComments removes YAML `#` lines and Helm `{{/* */}}` blocks
// (including go-template `{{- /* */ -}}` variants), preserving the
// surrounding whitespace structure for later passes.
func stripComments(s string) string {
	// Block comments first; greedy across lines.
	blockRe := regexp.MustCompile(`(?s)\{\{-?\s*/\*.*?\*/\s*-?\}\}`)
	s = blockRe.ReplaceAllString(s, "")

	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		// Drop the first `#` not inside a quoted string. Cheap parse:
		// honor double quotes and single quotes; skip escape pairs.
		idx := -1
		inDouble, inSingle := false, false
		for i := 0; i < len(line); i++ {
			c := line[i]
			switch {
			case c == '\\' && i+1 < len(line):
				i++ // skip escaped char
			case c == '"' && !inSingle:
				inDouble = !inDouble
			case c == '\'' && !inDouble:
				inSingle = !inSingle
			case c == '#' && !inDouble && !inSingle:
				idx = i
			}
			if idx >= 0 {
				break
			}
		}
		if idx >= 0 {
			line = strings.TrimRight(line[:idx], " \t")
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	out := b.String()
	if strings.HasSuffix(out, "\n") && !strings.HasSuffix(s, "\n") {
		out = strings.TrimSuffix(out, "\n")
	}
	return out
}

// truncateLongFields cuts any line over maxLen with a " …<truncated>"
// suffix and signals whether a cut happened.
func truncateLongFields(s string, maxLen int) (string, bool) {
	if maxLen <= 0 {
		return s, false
	}
	var b strings.Builder
	truncated := false
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		if len(line) <= maxLen {
			b.WriteString(line)
			continue
		}
		b.WriteString(line[:maxLen])
		b.WriteString(" …<truncated>")
		truncated = true
	}
	return b.String(), truncated
}

// wrapUserData pairs with the system-prompt instruction to treat the
// wrapped content as untrusted plain text.
func wrapUserData(s string) string {
	return "<USER_DATA>\n" + s + "\n</USER_DATA>"
}
