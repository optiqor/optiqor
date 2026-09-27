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

func TestState_Advance(t *testing.T) {
	for _, tc := range []struct {
		name       string
		seed       func() *State
		next       Stage
		at         time.Time
		wantErr    error
		wantCur    Stage
		extraCheck func(t *testing.T, s *State)
	}{
		{
			name:    "forward step",
			seed:    func() *State { s := New(time.Unix(1, 0)); return &s },
			next:    StageVCSConnected,
			at:      time.Unix(2, 0),
			wantCur: StageVCSConnected,
		},
		{
			name: "chained forward steps",
			seed: func() *State {
				s := New(time.Unix(1, 0))
				_ = s.Advance(StageVCSConnected, time.Unix(2, 0))
				return &s
			},
			next:    StageFirstPRAnalyzed,
			at:      time.Unix(3, 0),
			wantCur: StageFirstPRAnalyzed,
		},
		{
			name:    "forward skip allowed",
			seed:    func() *State { s := New(time.Unix(1, 0)); return &s },
			next:    StageFirstApplyFix,
			at:      time.Unix(2, 0),
			wantCur: StageFirstApplyFix,
		},
		{
			name: "rewind rejected and leaves state intact",
			seed: func() *State {
				s := New(time.Unix(1, 0))
				_ = s.Advance(StageAgentInstalled, time.Unix(2, 0))
				return &s
			},
			next:    StageVCSConnected,
			at:      time.Unix(3, 0),
			wantErr: ErrIllegalTransition,
			wantCur: StageAgentInstalled,
		},
		{
			name:    "unknown stage rejected",
			seed:    func() *State { s := New(time.Unix(1, 0)); return &s },
			next:    Stage("nope"),
			at:      time.Unix(2, 0),
			wantErr: ErrIllegalTransition,
			wantCur: StageSignedUp,
		},
		{
			name: "re-arrival is idempotent on first-arrival timestamp",
			seed: func() *State {
				s := New(time.Unix(1, 0))
				_ = s.Advance(StageVCSConnected, time.Unix(2, 0))
				return &s
			},
			next:    StageVCSConnected,
			at:      time.Unix(99, 0),
			wantCur: StageVCSConnected,
			extraCheck: func(t *testing.T, s *State) {
				t.Helper()
				if !s.Reached[StageVCSConnected].Equal(time.Unix(2, 0)) {
					t.Errorf("Reached should record first arrival only, got %v", s.Reached[StageVCSConnected])
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.seed()
			err := s.Advance(tc.next, tc.at)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err: got %v want %v", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if s.Current != tc.wantCur {
				t.Errorf("Current: got %v want %v", s.Current, tc.wantCur)
			}
			if tc.extraCheck != nil {
				tc.extraCheck(t, s)
			}
		})
	}
}

func TestState_Activated(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed func() State
		want bool
	}{
		{
			name: "within window",
			seed: func() State {
				s := New(time.Unix(0, 0))
				_ = s.Advance(StageFirstApplyFix, time.Unix(0, 0).Add(13*24*time.Hour))
				return s
			},
			want: true,
		},
		{
			name: "past window",
			seed: func() State {
				s := New(time.Unix(0, 0))
				_ = s.Advance(StageFirstApplyFix, time.Unix(0, 0).Add(20*24*time.Hour))
				return s
			},
			want: false,
		},
		{
			name: "no apply fix",
			seed: func() State { return New(time.Unix(0, 0)) },
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.seed()
			if got := s.Activated(SLOActivationWindow); got != tc.want {
				t.Errorf("Activated: got %v want %v", got, tc.want)
			}
		})
	}
}

func TestState_TimeToFirstReceipt(t *testing.T) {
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
		// 31d - 1h is comfortably under the 35d SLO.
		t.Error("31d should be healthy (<= 35d SLO)")
	}
}

func TestState_HealthyTimeToFirstReceipt_LateUnhealthy(t *testing.T) {
	s := New(time.Unix(0, 0))
	_ = s.Advance(StageAgentInstalled, time.Unix(0, 0))
	_ = s.Advance(StageFirstReceipt, time.Unix(0, 0).Add(40*24*time.Hour))
	if s.HealthyTimeToFirstReceipt() {
		t.Error("40d should be unhealthy")
	}
}

func TestState_ProgressPercent(t *testing.T) {
	for _, tc := range []struct {
		name string
		seed func() State
		want int
	}{
		{
			name: "signed up",
			seed: func() State { return New(time.Unix(0, 0)) },
			want: 0,
		},
		{
			name: "first receipt",
			seed: func() State {
				s := New(time.Unix(0, 0))
				_ = s.Advance(StageFirstReceipt, time.Unix(1, 0))
				return s
			},
			want: 100,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.seed()
			if got := s.ProgressPercent(); got != tc.want {
				t.Errorf("ProgressPercent: got %d want %d", got, tc.want)
			}
		})
	}
}

func TestStages_KeysMatchSchema(t *testing.T) {
	// Drift here breaks tenants.onboarding_state JSONB readers.
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
