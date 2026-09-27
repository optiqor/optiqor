// Package classify is the statistical workload classifier. Pure
// functions, no I/O per ADR-0006 (internal/methodology/* must not
// import other internal/* packages). The real classifier ships in
// Phase 6; SandboxClassifier is the placeholder so callers can wire
// today.
package classify
