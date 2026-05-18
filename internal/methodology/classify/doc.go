// Package classify is the statistical workload classifier. It groups a
// workload's observed CPU and memory time-series into one of four
// behavior classes — steady, daily-cyclical, weekly-cyclical, bursty —
// based on coefficient of variation and autocorrelation at the 24-hour
// and 168-hour lags. The classification feeds the sizing engine: each
// class has a different percentile target and safety-margin policy
// (steady → P95 + 30%, cyclical → seasonal-max P95 + 25%, bursty →
// P99 + 50%).
//
// This package lives under internal/methodology/ per ADR-0006: pure
// functions, no I/O, deterministic, inputs explicit. The lint rule
// preventing internal/methodology/* from importing other internal/*
// packages applies — see ADR-0006 §"What we add on top".
//
// Status: scaffolding. The real classifier ships in Phase 6 alongside
// the rest of internal/methodology/. This package is a placeholder so
// callers (cost attribution, sizing engine) can wire against the
// interface today while the implementation is written.
package classify
