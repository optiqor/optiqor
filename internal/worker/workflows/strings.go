package workflows

import "strings"

// stringReader exists so workflows don't import the heavy bytes/io
// surface piecemeal. Kept tiny: one func, exact shape parser wants.
func stringReader(s string) *strings.Reader { return strings.NewReader(s) }
