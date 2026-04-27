// Package onboarding models the tenant onboarding state machine.
//
// Each tenant moves through a fixed sequence of stages from signup to
// first verified Receipt. The schema persists the current stage plus
// per-stage timestamps in tenants.onboarding_state JSONB; this package
// defines the type, the legal transitions, and the activation funnel
// math the metrics package consumes.
//
// Hard time-to-value SLOs (committed in todo.md production-readiness
// gap #1) live as constants here so the renderer, nudge workflows,
// and dashboards all read the same source.
package onboarding

import (
	"errors"
	"fmt"
	"time"
)

// Stage is the tenant's current onboarding position.
type Stage string

const (
	StageSignedUp        Stage = "signed_up"
	StageVCSConnected    Stage = "vcs_connected"
	StageRepoSelected    Stage = "repo_selected"
	StageFirstPRAnalyzed Stage = "first_pr_analyzed"
	StageAgentInstalled  Stage = "agent_installed"
	StageFirstApplyFix   Stage = "first_apply_fix"
	StageFirstReceipt    Stage = "first_receipt_issued"
)

// Stages is the ordered list — `index` doubles as a numeric progress
// score and is what the customer-visible "you are here" indicator uses.
var Stages = []Stage{
	StageSignedUp,
	StageVCSConnected,
	StageRepoSelected,
	StageFirstPRAnalyzed,
	StageAgentInstalled,
	StageFirstApplyFix,
	StageFirstReceipt,
}

// Index returns the position of s in Stages, or -1 if unknown.
func Index(s Stage) int {
	for i, v := range Stages {
		if v == s {
			return i
		}
	}
	return -1
}

// SLO targets, in product. Phase 5 enforces these as P1 incidents
// when violated for any tenant.
const (
	SLOSandboxLatency        = 3 * time.Second     // sandbox p95
	SLOInstallToFirstPR      = 10 * time.Minute    // install → first PR comment
	SLOInstallToFirstReco    = 30 * time.Minute    // agent install → first recommendation
	SLOInstallToFirstReceipt = 35 * 24 * time.Hour // 35 days p50
	SLOActivationWindow      = 14 * 24 * time.Hour // window for "activated" definition
)

// State is the per-tenant onboarding record. Persisted as JSONB on
// tenants.onboarding_state.
type State struct {
	Current Stage `json:"current"`
	// Reached records the timestamp of the first transition into each
	// stage. Stages can be revisited (e.g. customer reinstalls the
	// agent) — only the first arrival counts toward activation math.
	Reached map[Stage]time.Time `json:"reached,omitempty"`
}

// New returns a fresh State at StageSignedUp with the given timestamp.
func New(t time.Time) State {
	return State{
		Current: StageSignedUp,
		Reached: map[Stage]time.Time{StageSignedUp: t},
	}
}

// ErrIllegalTransition is returned by Advance when a transition would
// move backward or skip to an unknown stage.
var ErrIllegalTransition = errors.New("onboarding: illegal stage transition")

// Advance moves the tenant forward to next at time t. Skipping
// intermediate stages is allowed (a customer can land on an Apply Fix
// before we've recorded VCS connection in some onboarding paths) but
// going backwards is rejected.
//
// Re-arriving at the same stage is a no-op (idempotent).
func (s *State) Advance(next Stage, t time.Time) error {
	curIdx, nextIdx := Index(s.Current), Index(next)
	if nextIdx < 0 {
		return fmt.Errorf("%w: unknown stage %q", ErrIllegalTransition, next)
	}
	if nextIdx < curIdx {
		return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, s.Current, next)
	}
	s.Current = next
	if s.Reached == nil {
		s.Reached = map[Stage]time.Time{}
	}
	if _, recorded := s.Reached[next]; !recorded {
		s.Reached[next] = t
	}
	return nil
}

// Activated reports whether the tenant has reached StageFirstApplyFix
// within window from signup. This is the canonical activation metric
// (target: ≥ 60% in Y1).
func (s State) Activated(window time.Duration) bool {
	signup, ok := s.Reached[StageSignedUp]
	if !ok {
		return false
	}
	apply, ok := s.Reached[StageFirstApplyFix]
	if !ok {
		return false
	}
	return apply.Sub(signup) <= window
}

// TimeToFirstReceipt returns (duration, true) if the tenant has issued
// a Receipt; (0, false) otherwise.
func (s State) TimeToFirstReceipt() (time.Duration, bool) {
	install, hasInstall := s.Reached[StageAgentInstalled]
	receipt, hasReceipt := s.Reached[StageFirstReceipt]
	if !hasInstall || !hasReceipt {
		return 0, false
	}
	return receipt.Sub(install), true
}

// HealthyTimeToFirstReceipt reports whether the tenant met the p50 SLO
// (≤ 35 days from agent install). Used by the leading-churn metric.
func (s State) HealthyTimeToFirstReceipt() bool {
	d, ok := s.TimeToFirstReceipt()
	if !ok {
		return false
	}
	return d <= SLOInstallToFirstReceipt
}

// ProgressPercent returns 0..100 based on how far the tenant has
// advanced through Stages. Surfaces in the customer-visible "you are
// here" indicator.
func (s State) ProgressPercent() int {
	idx := Index(s.Current)
	if idx < 0 {
		return 0
	}
	return (idx * 100) / (len(Stages) - 1)
}
