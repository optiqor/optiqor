package onboarding

import (
	"errors"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	now := time.Now()
	s := New(now)
	if s.Current != StageSignedUp {
		t.Errorf("New().Current = %v", s.Current)
	}
	if got, ok := s.Reached[StageSignedUp]; !ok || !got.Equal(now) {
		t.Errorf("Reached[signed_up] = %v ok=%v", got, ok)
	}
}

func TestAdvance_Forward(t *testing.T) {
	s := New(time.Unix(1, 0))
	if err := s.Advance(StageVCSConnected, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	if err := s.Advance(StageFirstPRAnalyzed, time.Unix(3, 0)); err != nil {
		t.Fatal(err)
	}
	if s.Current != StageFirstPRAnalyzed {
		t.Errorf("Current = %v", s.Current)
	}
}

func TestAdvance_SkipAllowed(t *testing.T) {
	s := New(time.Unix(1, 0))
	if err := s.Advance(StageFirstApplyFix, time.Unix(2, 0)); err != nil {
		t.Fatalf("forward skip should be allowed: %v", err)
	}
	if s.Current != StageFirstApplyFix {
		t.Errorf("Current = %v", s.Current)
	}
}

func TestAdvance_RewindRejected(t *testing.T) {
	s := New(time.Unix(1, 0))
	_ = s.Advance(StageAgentInstalled, time.Unix(2, 0))
	err := s.Advance(StageVCSConnected, time.Unix(3, 0))
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("rewind should error, got %v", err)
	}
	if s.Current != StageAgentInstalled {
		t.Errorf("rewind should not mutate state")
	}
}

func TestAdvance_UnknownStageRejected(t *testing.T) {
	s := New(time.Unix(1, 0))
	if err := s.Advance(Stage("nope"), time.Unix(2, 0)); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("unknown stage should error, got %v", err)
	}
}

func TestAdvance_Idempotent(t *testing.T) {
	s := New(time.Unix(1, 0))
	_ = s.Advance(StageVCSConnected, time.Unix(2, 0))
	first := s.Reached[StageVCSConnected]
	_ = s.Advance(StageVCSConnected, time.Unix(99, 0))
	if !s.Reached[StageVCSConnected].Equal(first) {
		t.Error("Reached should record first arrival only")
	}
}

func TestActivated_Yes(t *testing.T) {
	s := New(time.Unix(0, 0))
	_ = s.Advance(StageFirstApplyFix, time.Unix(0, 0).Add(13*24*time.Hour))
	if !s.Activated(SLOActivationWindow) {
		t.Error("13d after signup should activate within 14d window")
	}
}

func TestActivated_LateNo(t *testing.T) {
	s := New(time.Unix(0, 0))
	_ = s.Advance(StageFirstApplyFix, time.Unix(0, 0).Add(20*24*time.Hour))
	if s.Activated(SLOActivationWindow) {
		t.Error("20d should NOT activate within 14d window")
	}
}

func TestActivated_NoApplyFix(t *testing.T) {
	s := New(time.Unix(0, 0))
	if s.Activated(SLOActivationWindow) {
		t.Error("no apply_fix → not activated")
	}
}

func TestTimeToFirstReceipt(t *testing.T) {
	s := New(time.Unix(0, 0))
	_ = s.Advance(StageAgentInstalled, time.Unix(0, 0).Add(time.Hour))
	_ = s.Advance(StageFirstReceipt, time.Unix(0, 0).Add(31*24*time.Hour))

	d, ok := s.TimeToFirstReceipt()
	if !ok {
		t.Fatal("expected receipt time")
	}
	want := 31*24*time.Hour - time.Hour
	if d != want {
		t.Errorf("d = %v, want %v", d, want)
	}
	if !s.HealthyTimeToFirstReceipt() {
		t.Error("31d ≈ 30d after install should be healthy (≤ 35d)")
	}
}

func TestHealthyTimeToFirstReceipt_LateUnhealthy(t *testing.T) {
	s := New(time.Unix(0, 0))
	_ = s.Advance(StageAgentInstalled, time.Unix(0, 0))
	_ = s.Advance(StageFirstReceipt, time.Unix(0, 0).Add(40*24*time.Hour))
	if s.HealthyTimeToFirstReceipt() {
		t.Error("40d should be unhealthy")
	}
}

func TestProgressPercent(t *testing.T) {
	s := New(time.Unix(0, 0))
	if got := s.ProgressPercent(); got != 0 {
		t.Errorf("ProgressPercent at signed_up = %d, want 0", got)
	}
	_ = s.Advance(StageFirstReceipt, time.Unix(1, 0))
	if got := s.ProgressPercent(); got != 100 {
		t.Errorf("ProgressPercent at first_receipt = %d, want 100", got)
	}
}

func TestStages_KeysMatchSchema(t *testing.T) {
	// Regression guard: any change to the stage list must remain in
	// sync with tenants.onboarding_state JSONB readers.
	want := []Stage{
		StageSignedUp,
		StageVCSConnected,
		StageRepoSelected,
		StageFirstPRAnalyzed,
		StageAgentInstalled,
		StageFirstApplyFix,
		StageFirstReceipt,
	}
	if len(Stages) != len(want) {
		t.Fatalf("Stages drift: %d vs %d", len(Stages), len(want))
	}
	for i := range want {
		if Stages[i] != want[i] {
			t.Errorf("Stages[%d] = %v, want %v", i, Stages[i], want[i])
		}
	}
}
