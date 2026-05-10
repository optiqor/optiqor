// Package parser is a thin re-export of the optiqor-cli public parser
// package. The CLI repo owns the canonical Helm-values normaliser; the
// backend imports those types verbatim so SaaS detections operate on
// the same Workload shape as the offline CLI.
//
// Adding a field here is a sign you should add it to
// github.com/optiqor/optiqor-cli/pkg/parser first — see backend
// CLAUDE.md ("Don't fork CLI types").
package parser

import (
	"errors"
	"io"

	cliparser "github.com/optiqor/optiqor-cli/pkg/parser"
)

// ErrParse is the sentinel returned (wrapped) by ParseValues so
// handlers can write a single `errors.Is(err, parser.ErrParse)` branch
// for HTTP 400 mapping.
var ErrParse = errors.New("parser: malformed values")

// Re-exported types. New consumers should reference these via this
// package so a future swap of the upstream import path is a one-file
// change.
type (
	Workload        = cliparser.Workload
	SecurityContext = cliparser.SecurityContext
	ResourceList    = cliparser.ResourceList
	ImageRef        = cliparser.ImageRef
	Quantity        = cliparser.Quantity
)

// ParseValues normalises a Helm `values.yaml` (or merged values stream)
// into the canonical Workload list. Errors are wrapped with a backend
// sentinel so handlers can distinguish parser failures from anything
// else.
func ParseValues(r io.Reader) ([]Workload, error) {
	ws, err := cliparser.ParseValues(r)
	if err != nil {
		return nil, &Error{Cause: err}
	}
	return ws, nil
}

// Error wraps an upstream parser error so backend HTTP handlers can
// reject malformed input as 400 without spreading the type assertion
// throughout the call site. errors.Is(err, ErrParse) returns true.
type Error struct{ Cause error }

func (e *Error) Error() string { return "parser: " + e.Cause.Error() }
func (e *Error) Unwrap() error { return e.Cause }
func (e *Error) Is(target error) bool {
	return target == ErrParse
}
