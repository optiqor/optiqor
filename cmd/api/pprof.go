package main

import (
	"crypto/subtle"
	"net/http"
	"net/http/pprof"
)

// pprof handler aliases — kept here so main.go imports stay focused.
var (
	pprofIndex   = pprof.Index
	pprofCmdline = pprof.Cmdline
	pprofProfile = pprof.Profile
	pprofSymbol  = pprof.Symbol
	pprofTrace   = pprof.Trace
)

// subtleConstantTimeEq is a fixed-time string equality check.
// Returns 1 when equal, 0 otherwise. Wraps subtle.ConstantTimeCompare
// with a length check that itself runs in constant time.
func subtleConstantTimeEq(a, b string) int {
	if len(a) != len(b) {
		return 0
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b))
}

// _ ensures the pprof package is imported even when no one calls the
// handlers — this keeps the import meaningful for go vet.
var _ = http.NotFound
