package main

import (
	"crypto/subtle"
	"net/http"
	"net/http/pprof"
)

var (
	pprofIndex   = pprof.Index
	pprofCmdline = pprof.Cmdline
	pprofProfile = pprof.Profile
	pprofSymbol  = pprof.Symbol
	pprofTrace   = pprof.Trace
)

// subtleConstantTimeEq returns 1 when a == b, 0 otherwise. The length
// check is also constant-time so a length mismatch doesn't leak via
// timing on the admin-token gate (cmd/api/main.go mountPProf).
func subtleConstantTimeEq(a, b string) int {
	if len(a) != len(b) {
		return 0
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b))
}

var _ = http.NotFound
