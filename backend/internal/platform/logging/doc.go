// Package logging provides the slog handler that auto-injects
// tenant_id, request_id, and workflow_id from context into every log
// line. The injection is the contract: a log line missing tenant_id is
// an upstream bug, not a logger bug.
package logging
