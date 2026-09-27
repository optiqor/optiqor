package dashboard

import (
	"testing"
	"time"
)

func TestDegradeStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stored string
		fresh  int
		want   string
	}{
		{"healthy and fresh stays healthy", "healthy", 30, "healthy"},
		{"healthy past 1h flips to degraded", "healthy", 3700, "degraded"},
		{"healthy past 6h flips to offline", "healthy", 7 * 3600, "offline"},
		{"degraded past 6h flips to offline", "degraded", 7 * 3600, "offline"},
		{"degraded under 6h stays degraded", "degraded", 2 * 3600, "degraded"},
		{"offline stays offline regardless", "offline", 30, "offline"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := degradeStatus(tc.stored, tc.fresh); got != tc.want {
				t.Errorf("degradeStatus(%q, %d) = %q, want %q", tc.stored, tc.fresh, got, tc.want)
			}
		})
	}
}

func TestAgentHealthPgStore_RecomputeFreshness(t *testing.T) {
	base := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	s := &AgentHealthPgStore{Now: func() time.Time { return base.Add(120 * time.Second) }}

	if got := s.recomputeFreshness(base, 60); got != 120 {
		t.Errorf("wall-clock 120s should beat reported 60s; got %d", got)
	}
	if got := s.recomputeFreshness(base, 300); got != 300 {
		t.Errorf("reported 300s should beat wall-clock 120s; got %d", got)
	}
	// Future last_seen (clock skew) clamps to zero rather than going negative.
	future := base.Add(600 * time.Second) // 480s ahead of the stub now
	if got := s.recomputeFreshness(future, 0); got != 0 {
		t.Errorf("future last_seen should clamp wall to 0; got %d", got)
	}
}
