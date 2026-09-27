package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMemory_Allow(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "allows up to limit then blocks",
			run: func(t *testing.T) {
				t.Helper()
				now := time.Unix(1_700_000_000, 0)
				lim := NewMemory(3, time.Minute).WithClock(func() time.Time { return now })

				for i := 0; i < 3; i++ {
					ok, retry, err := lim.Allow(context.Background(), "ip-1")
					if err != nil || !ok {
						t.Fatalf("request %d: want allow, got ok=%v retry=%v err=%v", i, ok, retry, err)
					}
				}
				ok, retry, err := lim.Allow(context.Background(), "ip-1")
				if err != nil {
					t.Fatalf("err: %v", err)
				}
				if ok {
					t.Error("4th request should be blocked")
				}
				if retry <= 0 || retry > time.Minute {
					t.Errorf("retry: want (0, 60s], got %v", retry)
				}
			},
		},
		{
			name: "window expiry resets count",
			run: func(t *testing.T) {
				t.Helper()
				clock := time.Unix(1_700_000_000, 0)
				lim := NewMemory(1, time.Second).WithClock(func() time.Time { return clock })

				if ok, _, _ := lim.Allow(context.Background(), "k"); !ok {
					t.Fatal("first request must be allowed")
				}
				if ok, _, _ := lim.Allow(context.Background(), "k"); ok {
					t.Fatal("second request in same window must be blocked")
				}

				clock = clock.Add(2 * time.Second)
				if ok, _, _ := lim.Allow(context.Background(), "k"); !ok {
					t.Error("request after window expiry must be allowed (counter reset)")
				}
			},
		},
		{
			// limit=2 lets us prove independence: drain key "a" to its
			// cap and confirm key "b" still has its full quota.
			name: "keys are independent",
			run: func(t *testing.T) {
				t.Helper()
				lim := NewMemory(2, time.Minute)

				for i := 0; i < 2; i++ {
					if ok, _, _ := lim.Allow(context.Background(), "a"); !ok {
						t.Fatalf("a request %d should be allowed", i)
					}
				}
				if blocked, _, _ := lim.Allow(context.Background(), "a"); blocked {
					t.Error("3rd hit on key a should be blocked")
				}
				for i := 0; i < 2; i++ {
					if ok, _, _ := lim.Allow(context.Background(), "b"); !ok {
						t.Errorf("b request %d should be allowed independently of a", i)
					}
				}
			},
		},
		{
			name: "lazy sweep drops expired",
			run: func(t *testing.T) {
				t.Helper()
				clock := time.Unix(1_700_000_000, 0)
				lim := NewMemory(1, time.Second).WithClock(func() time.Time { return clock })

				for _, k := range []string{"a", "b", "c"} {
					_, _, _ = lim.Allow(context.Background(), k)
				}
				if got := lim.Len(); got != 3 {
					t.Fatalf("want 3 keys tracked, got %d", got)
				}

				clock = clock.Add(2 * time.Second)
				_, _, _ = lim.Allow(context.Background(), "d") // triggers sweep
				if got := lim.Len(); got != 1 {
					t.Errorf("sweep should have evicted expired keys; got %d, want 1", got)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) { tc.run(t) })
	}
}

func TestNewMemory_PanicsOnInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  int
		window time.Duration
	}{
		{"zero limit", 0, time.Second},
		{"negative limit", -1, time.Second},
		{"zero window", 1, 0},
		{"negative window", 1, -time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("expected panic for %+v", tc)
				}
			}()
			_ = NewMemory(tc.limit, tc.window)
		})
	}
}

// stubLimiter lets us inject any (allow, retry, err) tuple to exercise
// every Middleware code path without a real limiter.
type stubLimiter struct {
	allow bool
	retry time.Duration
	err   error
}

func (s stubLimiter) Allow(_ context.Context, _ string) (bool, time.Duration, error) {
	return s.allow, s.retry, s.err
}

func TestMiddleware(t *testing.T) {
	for _, tc := range []struct {
		name       string
		opts       Options
		wantStatus int
		wantHeader string // Retry-After; "" means don't check
		wantCalled bool
	}{
		{
			name:       "passes allowed",
			opts:       Options{Limiter: stubLimiter{allow: true}, KeyFn: func(r *http.Request) string { return "k" }},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name: "blocks 429 with retry-after",
			opts: Options{
				Limiter: stubLimiter{allow: false, retry: 5 * time.Second},
				KeyFn:   func(r *http.Request) string { return "k" },
			},
			wantStatus: http.StatusTooManyRequests,
			wantHeader: "5",
		},
		{
			name: "fail open on limiter error",
			opts: Options{
				Limiter:  stubLimiter{err: errors.New("redis down")},
				KeyFn:    func(r *http.Request) string { return "k" },
				FailOpen: true,
			},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
		{
			name: "fail closed returns 503 on error",
			opts: Options{
				Limiter:  stubLimiter{err: errors.New("redis down")},
				KeyFn:    func(r *http.Request) string { return "k" },
				FailOpen: false,
			},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			// Empty key falls through so a misconfigured proxy doesn't
			// block all traffic onto one bucket.
			name: "empty key bypasses",
			opts: Options{
				Limiter: stubLimiter{allow: false},
				KeyFn:   func(r *http.Request) string { return "" },
			},
			wantStatus: http.StatusOK,
			wantCalled: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})
			mw := Middleware(tc.opts)
			rec := httptest.NewRecorder()
			mw(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))

			if rec.Code != tc.wantStatus {
				t.Errorf("status: got %d want %d", rec.Code, tc.wantStatus)
			}
			if called != tc.wantCalled {
				t.Errorf("next called: got %v want %v", called, tc.wantCalled)
			}
			if tc.wantHeader != "" {
				if got := rec.Header().Get("Retry-After"); got != tc.wantHeader {
					t.Errorf("Retry-After: got %q want %q", got, tc.wantHeader)
				}
			}
		})
	}
}

func TestMiddleware_PanicsWithoutLimiter(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("constructor without Limiter should panic")
		}
	}()
	_ = Middleware(Options{})
}

func TestClientIPKey_PrefersXFF(t *testing.T) {
	for _, tc := range []struct {
		name   string
		xff    string
		remote string
		want   string
	}{
		{"xff single", "1.2.3.4", "10.0.0.1:5000", "1.2.3.4"},
		{"xff chain leftmost wins", "1.2.3.4, 5.6.7.8, 9.10.11.12", "10.0.0.1:5000", "1.2.3.4"},
		{"xff leading comma falls back", ", 5.6.7.8", "10.0.0.1:5000", "5.6.7.8"},
		{"no xff falls back to remoteaddr host", "", "10.0.0.1:5000", "10.0.0.1"},
		{"remoteaddr without port returned as-is", "", "unix-socket", "unix-socket"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			r.RemoteAddr = tc.remote
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := ClientIPKey(r); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
