package activation

import (
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/onboarding"
)

func mkState(signup time.Time, reached map[onboarding.Stage]time.Time) onboarding.State {
	r := map[onboarding.Stage]time.Time{onboarding.StageSignedUp: signup}
	for k, v := range reached {
		r[k] = v
	}
	cur := onboarding.StageSignedUp
	for _, s := range onboarding.Stages {
		if _, ok := r[s]; ok {
			cur = s
		}
	}
	return onboarding.State{Current: cur, Reached: r}
}

func TestActivationRate_NoSamples(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	got := ActivationRate(nil, 0, now)
	if got.Sample != 0 || got.Activated != 0 || got.Percent != 0 {
		t.Errorf("zero snapshots = %+v, want all zero", got)
	}
}

func TestActivationRate_WindowFiltersOldTenants(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	window := 30 * 24 * time.Hour
	cohortInside := now.Add(-2 * 24 * time.Hour)
	cohortOutside := now.Add(-60 * 24 * time.Hour)

	snaps := []TenantSnapshot{
		{TenantID: "in", State: mkState(cohortInside, map[onboarding.Stage]time.Time{
			onboarding.StageFirstApplyFix: cohortInside.Add(2 * time.Hour),
		})},
		{TenantID: "out", State: mkState(cohortOutside, map[onboarding.Stage]time.Time{
			onboarding.StageFirstApplyFix: cohortOutside.Add(2 * time.Hour),
		})},
	}
	got := ActivationRate(snaps, window, now)
	if got.Sample != 1 || got.Activated != 1 {
		t.Errorf("got %+v, want sample=1 activated=1 (old tenant filtered)", got)
	}
}

func TestActivationRate_OnlyApplyFixWithinSLOActivates(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	earlySignup := now.Add(-3 * 24 * time.Hour)

	snaps := []TenantSnapshot{
		{TenantID: "fast", State: mkState(earlySignup, map[onboarding.Stage]time.Time{
			onboarding.StageFirstApplyFix: earlySignup.Add(48 * time.Hour),
		})},
		{TenantID: "slow", State: mkState(earlySignup, map[onboarding.Stage]time.Time{
			onboarding.StageFirstApplyFix: earlySignup.Add(20 * 24 * time.Hour),
		})},
		{TenantID: "noapply", State: mkState(earlySignup, nil)},
	}
	got := ActivationRate(snaps, 0, now)
	if got.Sample != 3 {
		t.Errorf("sample = %d, want 3", got.Sample)
	}
	if got.Activated != 1 {
		t.Errorf("activated = %d, want 1 (only the fast one)", got.Activated)
	}
	if got.Percent < 33.0 || got.Percent > 33.5 {
		t.Errorf("percent = %.2f, want ~33.33", got.Percent)
	}
}

func TestActivationRate_FallsBackToCreatedAtWhenNoSignupReached(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	created := now.Add(-1 * 24 * time.Hour)

	// State has no StageSignedUp in Reached — Phase-1 pre-machine tenant.
	st := onboarding.State{Current: onboarding.StageVCSConnected, Reached: map[onboarding.Stage]time.Time{}}
	snaps := []TenantSnapshot{{TenantID: "legacy", State: st, CreatedAt: created}}

	got := ActivationRate(snaps, 30*24*time.Hour, now)
	if got.Sample != 1 {
		t.Errorf("legacy tenant should be sampled via CreatedAt; got sample=%d", got.Sample)
	}
}

func TestTimeToFirstReceipt_Percentiles(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	mk := func(install, receipt time.Duration) TenantSnapshot {
		signup := now.Add(-60 * 24 * time.Hour)
		return TenantSnapshot{State: mkState(signup, map[onboarding.Stage]time.Time{
			onboarding.StageAgentInstalled: signup.Add(install),
			onboarding.StageFirstReceipt:   signup.Add(install + receipt),
		})}
	}
	snaps := []TenantSnapshot{
		mk(time.Hour, 5*24*time.Hour),
		mk(time.Hour, 20*24*time.Hour),
		mk(time.Hour, 35*24*time.Hour),
		mk(time.Hour, 50*24*time.Hour),
	}
	ttfr := TimeToFirstReceipt(snaps)
	if ttfr.Sample != 4 {
		t.Errorf("sample = %d, want 4", ttfr.Sample)
	}
	// Nearest-rank: p50 of 4 samples is rank 2 → 20d.
	if want := 20 * 24 * time.Hour; ttfr.P50 != want {
		t.Errorf("p50 = %s, want %s", ttfr.P50, want)
	}
	// p90 of 4 samples → rank ceil(3.6+0.5) = 4 → 50d.
	if want := 50 * 24 * time.Hour; ttfr.P90 != want {
		t.Errorf("p90 = %s, want %s", ttfr.P90, want)
	}
	if ttfr.Max != 50*24*time.Hour {
		t.Errorf("max = %s, want 50d", ttfr.Max)
	}
	// P50 = 20d ≤ SLOInstallToFirstReceipt (35d) → healthy.
	if !ttfr.HealthyP50 {
		t.Error("p50 = 20d should be HealthyP50 (SLO 35d)")
	}
}

func TestTimeToFirstReceipt_NoneReached(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	signup := now.Add(-7 * 24 * time.Hour)
	snaps := []TenantSnapshot{
		{State: mkState(signup, nil)},
	}
	ttfr := TimeToFirstReceipt(snaps)
	if ttfr.Sample != 0 {
		t.Errorf("sample = %d, want 0", ttfr.Sample)
	}
	if ttfr.HealthyP50 {
		t.Error("HealthyP50 must be false when no samples")
	}
}

func TestCompute_BundlesBoth(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	signup := now.Add(-1 * 24 * time.Hour)
	snaps := []TenantSnapshot{
		{State: mkState(signup, map[onboarding.Stage]time.Time{
			onboarding.StageAgentInstalled: signup.Add(1 * time.Hour),
			onboarding.StageFirstApplyFix:  signup.Add(2 * time.Hour),
			onboarding.StageFirstReceipt:   signup.Add(7 * 24 * time.Hour),
		})},
	}
	rpt := Compute(snaps, now)
	if rpt.Rate.Sample != 1 || rpt.Rate.Activated != 1 {
		t.Errorf("rate = %+v", rpt.Rate)
	}
	if rpt.TTFR.Sample != 1 {
		t.Errorf("ttfr = %+v", rpt.TTFR)
	}
	if !rpt.ComputedAt.Equal(now.UTC()) {
		t.Errorf("ComputedAt = %v, want %v", rpt.ComputedAt, now)
	}
}
