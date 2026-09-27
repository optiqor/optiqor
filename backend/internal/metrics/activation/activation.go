// Package activation computes cohort-level onboarding metrics: the
// Activation Rate (target ≥ 60% Y1) and Time-to-First-Receipt
// percentiles (target ≤ 35-day p50). Pure functions over a slice of
// tenant onboarding states; production wires a daily cron that pushes
// the result to Prometheus + the founder dashboard. See onboarding.SLO*
// for the policy thresholds.
package activation

import (
	"sort"
	"time"

	"github.com/optiqor/optiqor/internal/onboarding"
)

// TenantSnapshot is one row from the onboarding store. CreatedAt is the
// tenant row's created_at (used to size the cohort window when Reached
// is missing StageSignedUp — pre-Phase-5 tenants).
type TenantSnapshot struct {
	TenantID  string
	State     onboarding.State
	CreatedAt time.Time
}

// Rate carries the Activation cohort math. Window is the lookback the
// caller passed in; Sample is the count of tenants whose signup falls
// within (now - Window, now]. Activated is the subset that crossed
// StageFirstApplyFix inside onboarding.SLOActivationWindow of their
// own signup.
type Rate struct {
	Window    time.Duration
	Sample    int
	Activated int
	Percent   float64
}

// ActivationRate filters tenants whose signup falls in (now - window, now]
// and reports the fraction that activated within onboarding.SLOActivationWindow.
// Window <= 0 falls through to onboarding.SLOActivationWindow so the daily
// cron's most common case is a single argument.
func ActivationRate(snapshots []TenantSnapshot, window time.Duration, now time.Time) Rate {
	if window <= 0 {
		window = onboarding.SLOActivationWindow
	}
	r := Rate{Window: window}
	cohortFrom := now.Add(-window)
	for _, s := range snapshots {
		signup, ok := s.State.Reached[onboarding.StageSignedUp]
		if !ok {
			signup = s.CreatedAt
		}
		if signup.Before(cohortFrom) || signup.After(now) {
			continue
		}
		r.Sample++
		if s.State.Activated(onboarding.SLOActivationWindow) {
			r.Activated++
		}
	}
	if r.Sample > 0 {
		r.Percent = float64(r.Activated) / float64(r.Sample) * 100
	}
	return r
}

// TTFR summarises Time-to-First-Receipt over tenants who hit both
// StageAgentInstalled and StageFirstReceipt. Tenants missing either
// stage are excluded from the percentile math (not yet observable);
// they still count toward Total via the parent cohort.
type TTFR struct {
	Sample int
	P50    time.Duration
	P90    time.Duration
	Max    time.Duration
	// HealthyP50 is true when P50 is within onboarding.SLOInstallToFirstReceipt.
	// Drives the Phase-5 SLO gate.
	HealthyP50 bool
}

func TimeToFirstReceipt(snapshots []TenantSnapshot) TTFR {
	var durs []time.Duration
	for _, s := range snapshots {
		if d, ok := s.State.TimeToFirstReceipt(); ok {
			durs = append(durs, d)
		}
	}
	if len(durs) == 0 {
		return TTFR{}
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	t := TTFR{
		Sample: len(durs),
		P50:    percentile(durs, 0.50),
		P90:    percentile(durs, 0.90),
		Max:    durs[len(durs)-1],
	}
	t.HealthyP50 = t.P50 <= onboarding.SLOInstallToFirstReceipt
	return t
}

// percentile uses the nearest-rank method (no interpolation) so a
// single sample doesn't produce a spurious midpoint and tiny cohorts
// stay deterministic. Input must be sorted ascending.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	rank := int(p*float64(len(sorted)) + 0.5)
	if rank <= 0 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// Report bundles the two snapshots the cron emits each day.
type Report struct {
	ComputedAt time.Time
	Rate       Rate
	TTFR       TTFR
}

// Compute is the canonical one-call entry the daily cron uses.
func Compute(snapshots []TenantSnapshot, now time.Time) Report {
	return Report{
		ComputedAt: now.UTC(),
		Rate:       ActivationRate(snapshots, 0, now),
		TTFR:       TimeToFirstReceipt(snapshots),
	}
}
