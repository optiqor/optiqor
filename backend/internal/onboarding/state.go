// Package onboarding is the tenant onboarding state machine and its
// SLO constants. Stage + per-stage timestamps persist as JSONB on
// tenants.onboarding_state. SLOs (todo.md production-readiness gap #1)
// live here so renderer, nudge workflows, and dashboards agree.
package onboarding

import (
	"errors"
	"fmt"
	"time"
)

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

// Stages is ordered; the index doubles as the customer-visible "you
// are here" progress score.
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

// Time-to-value SLOs. Phase 5 pages on any tenant violation.
const (
	SLOSandboxLatency        = 3 * time.Second     // sandbox p95
	SLOInstallToFirstPR      = 10 * time.Minute    // install → first PR comment
	SLOInstallToFirstReco    = 30 * time.Minute    // install → first recommendation
	SLOInstallToFirstReceipt = 35 * 24 * time.Hour // 35d p50
	SLOActivationWindow      = 14 * 24 * time.Hour // "activated" window
)

type State struct {
	Current Stage `json:"current"`
	// Reached records first-arrival time per stage. Stages can be
	// revisited (agent reinstall), but only the first arrival counts
	// toward activation math.
	Reached map[Stage]time.Time `json:"reached,omitempty"`
}

// New returns a fresh State at StageSignedUp with the given timestamp.
func New(t time.Time) State {
	return State{
		Current: StageSignedUp,
		Reached: map[Stage]time.Time{StageSignedUp: t},
	}
}

var ErrIllegalTransition = errors.New("onboarding: illegal stage transition")

// Advance forwards to next at time t. Forward skips are allowed (some
// paths land on Apply Fix before we record VCS connection); rewinds
// are rejected. Re-arriving at the same stage is a no-op.
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

// Activated reports whether the tenant hit StageFirstApplyFix within
// window of signup. Canonical activation metric, Y1 target ≥ 60%.
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

// TimeToFirstReceipt returns the gap between agent install and first
// Receipt; ok=false until both stages are reached.
func (s State) TimeToFirstReceipt() (time.Duration, bool) {
	install, hasInstall := s.Reached[StageAgentInstalled]
	receipt, hasReceipt := s.Reached[StageFirstReceipt]
	if !hasInstall || !hasReceipt {
		return 0, false
	}
	return receipt.Sub(install), true
}

// HealthyTimeToFirstReceipt checks the 35-day p50 SLO. Feeds the
// leading-churn metric.
func (s State) HealthyTimeToFirstReceipt() bool {
	d, ok := s.TimeToFirstReceipt()
	if !ok {
		return false
	}
	return d <= SLOInstallToFirstReceipt
}

// ProgressPercent returns 0..100 based on Stage position; surfaces in
// the customer "you are here" indicator.
func (s State) ProgressPercent() int {
	idx := Index(s.Current)
	if idx < 0 {
		return 0
	}
	return (idx * 100) / (len(Stages) - 1)
}
