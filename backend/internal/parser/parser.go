// Package parser re-exports the optiqor-cli parser so SaaS detections
// operate on the same Workload shape as the offline CLI. New fields
// belong on github.com/optiqor/optiqor-cli/pkg/parser first; see
// backend CLAUDE.md ("Don't fork CLI types").
package parser

import (
	"errors"
	"io"

	cliparser "github.com/optiqor/optiqor-cli/pkg/parser"
)

// ErrParse is the sentinel ParseValues wraps so handlers can collapse
// every malformed-input case into one HTTP 400 branch.
var ErrParse = errors.New("parser: malformed values")

// Re-export via this package so a future upstream-import-path change is
// a one-file edit.
type (
	Workload        = cliparser.Workload
	SecurityContext = cliparser.SecurityContext
	ResourceList    = cliparser.ResourceList
	ImageRef        = cliparser.ImageRef
	Quantity        = cliparser.Quantity
)

// ParseValues normalises a Helm values stream into the canonical
// Workload list. Errors wrap ErrParse.
func ParseValues(r io.Reader) ([]Workload, error) {
	ws, err := cliparser.ParseValues(r)
	if err != nil {
		return nil, &Error{Cause: err}
	}
	return ws, nil
}

// Error wraps an upstream parser error so handlers don't spread the
// type assertion around. errors.Is(err, ErrParse) returns true.
type Error struct{ Cause error }

func (e *Error) Error() string { return "parser: " + e.Cause.Error() }
func (e *Error) Unwrap() error { return e.Cause }
func (e *Error) Is(target error) bool {
	return target == ErrParse
}
