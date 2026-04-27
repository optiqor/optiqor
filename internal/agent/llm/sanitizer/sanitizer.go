// Package sanitizer hardens LLM input against prompt-injection attacks
// from customer-controlled Helm values. It strips comments, detects
// injection markers, enforces per-field length limits, and wraps any
// remaining suspect text in <USER_DATA> boundaries that the LLM is
// instructed (in the system prompt) to never trust.
//
// This is Layer 1 of the LLM defence stack from todo.md
// production-readiness gap #6. Layer 2 (output validation) reuses the
// same render/conform/dryrun gate that Apply Fix uses.
package sanitizer

import (
	"errors"
	"regexp"
	"strings"
)

// Defaults that match production budgets. Tunable per-tenant later.
const (
	DefaultMaxFieldLen = 8 * 1024 // 8 KiB per Helm value field
	DefaultMaxTotalLen = 256 * 1024
)

// Result is the output of a sanitisation pass. The caller feeds Output
// to the prompt builder; if Suspicious is set, it should also surface
// the reason to the audit log and may down-rank the recommendation
// confidence one band per the LLM defense plan.
type Result struct {
	Output     string
	Suspicious bool
	Reasons    []string
	Truncated  bool
}

// Options tune the pass. Zero values default to the production budget.
type Options struct {
	MaxFieldLen int
	MaxTotalLen int
	// WrapSuspicious wraps the output in <USER_DATA>...</USER_DATA>
	// when injection markers are detected, so the LLM's system prompt
	// can treat it as untrusted text. Defaults to true.
	WrapSuspicious *bool
}

// Sanitize runs the pass. It is deterministic and side-effect free.
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

	// Injection-marker scan FIRST, against the raw input. Stripping
	// comments later would otherwise neutralise injections that
	// happened to sit in `#` comments — and we still want to flag
	// those because (a) it's evidence of intent, and (b) we should
	// down-rank confidence even when the output looks clean.
	if reasons := scanInjections(input); len(reasons) > 0 {
		r.Suspicious = true
		r.Reasons = append(r.Reasons, reasons...)
	}

	// Strip Helm-style line comments and block comments. We don't try
	// to be a full YAML parser; we operate line-wise.
	stripped := stripComments(input)

	// Field-length enforcement: any single line longer than MaxFieldLen
	// is truncated and flagged.
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

// injectionPatterns are case-insensitive regexes matching the most
// common prompt-injection markers. Customer-controlled comments
// containing these almost always indicate either a hostile actor or
// an honest mistake — both warrant the down-rank.
// Matchers are intentionally lenient about adjectives/connectives between
// the verb and the noun — real injection attempts vary widely in
// phrasing. False positives are acceptable here; they only down-rank
// confidence and surface a flag, never fail the analysis.
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

// stripComments removes:
//   - YAML / Helm `#` line comments (preserving the rest of the line)
//   - Helm `{{/* ... */}}` block comments
//   - Go-template `{{- /* ... */ -}}` variants
//
// Leaves the content otherwise intact so subsequent passes can keep
// operating on the original whitespace structure.
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

// truncateLongFields walks line by line; any line whose length exceeds
// maxLen is cut to maxLen + " …<truncated>". Returns the modified string
// and whether any truncation happened.
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

// wrapUserData wraps suspicious content in <USER_DATA>...</USER_DATA>
// markers. The system prompt instructs the LLM to treat anything
// inside these markers as untrusted plain text.
func wrapUserData(s string) string {
	return "<USER_DATA>\n" + s + "\n</USER_DATA>"
}
