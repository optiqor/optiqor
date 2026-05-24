package telemetry

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestCounter(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, r *Registry)
	}{
		{
			name: "increments and adds",
			run: func(t *testing.T, r *Registry) {
				t.Helper()
				c := r.NewCounter("optiqor_requests_total", "total requests", map[string]string{"service": "api"})
				c.Inc()
				c.Inc()
				c.Add(3.5)
				if got := c.Value(); got != 5.5 {
					t.Errorf("Value() = %v, want 5.5", got)
				}
			},
		},
		{
			name: "re-register returns existing",
			run: func(t *testing.T, r *Registry) {
				t.Helper()
				a := r.NewCounter("foo", "", map[string]string{"k": "v"})
				a.Inc()
				b := r.NewCounter("foo", "", map[string]string{"k": "v"})
				if a != b {
					t.Fatal("re-registration should return same counter; got distinct instances")
				}
				if got := b.Value(); got != 1 {
					t.Errorf("Value() = %v, want 1 (state persisted across re-register)", got)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, NewRegistry())
		})
	}
}

func TestHistogram_BucketsAndOverflow(t *testing.T) {
	r := NewRegistry()
	h := r.NewHistogram("optiqor_latency_seconds", "request latency", nil, []float64{0.1, 0.5, 1, 2, 5})
	for _, v := range []float64{0.05, 0.4, 0.6, 1.5, 6} {
		h.Observe(v)
	}
	snap := h.Snapshot()
	if snap.Count != 5 {
		t.Errorf("Count = %d, want 5", snap.Count)
	}
	wantPerBucket := []uint64{1, 1, 1, 1, 0}
	for i, want := range wantPerBucket {
		if snap.Counts[i] != want {
			t.Errorf("bucket[%d] = %d, want %d", i, snap.Counts[i], want)
		}
	}
	if snap.Counts[5] != 1 {
		t.Errorf("overflow bucket = %d, want 1", snap.Counts[5])
	}
	if snap.Sum != 0.05+0.4+0.6+1.5+6 {
		t.Errorf("Sum = %v", snap.Sum)
	}
}

func TestHistogram_NonAscendingPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on non-ascending boundaries")
		}
	}()
	NewRegistry().NewHistogram("h", "", nil, []float64{1, 0.5})
}

func TestRegistry_PrometheusText(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("optiqor_requests_total", "total requests", map[string]string{"method": "GET"})
	c.Add(7)

	h := r.NewHistogram("optiqor_latency_seconds", "request latency", nil, []float64{0.1, 1})
	h.Observe(0.05)
	h.Observe(2)

	var buf bytes.Buffer
	if err := r.WriteText(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`# HELP optiqor_requests_total total requests`,
		`# TYPE optiqor_requests_total counter`,
		`optiqor_requests_total{method="GET"} 7`,
		`# TYPE optiqor_latency_seconds histogram`,
		`optiqor_latency_seconds_bucket{le="0.1"} 1`,
		`optiqor_latency_seconds_bucket{le="+Inf"}`,
		`optiqor_latency_seconds_sum 2.05`,
		`optiqor_latency_seconds_count 2`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRegistry_Handler(t *testing.T) {
	r := NewRegistry()
	r.NewCounter("c", "", nil).Inc()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "c 1") {
		t.Errorf("body missing counter: %s", rec.Body.String())
	}
}

func TestNoopTracer(t *testing.T) {
	tr := NoopTracer()
	ctx, span := tr.Start(context.Background(), "op")
	if ctx == nil {
		t.Fatal("ctx must not be nil")
	}
	span.SetAttribute("k", "v")
	span.RecordError(errors.New("boom"))
	span.End()
}

func TestCounter_RaceSafe(t *testing.T) {
	r := NewRegistry()
	c := r.NewCounter("hot", "", nil)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Inc()
			}
		}()
	}
	wg.Wait()
	if got := c.Value(); got != 10_000 {
		t.Errorf("Value() = %v, want 10000", got)
	}
}

func TestHistogram_RaceSafe(t *testing.T) {
	r := NewRegistry()
	h := r.NewHistogram("h", "", nil, []float64{1})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				h.Observe(0.5)
			}
		}()
	}
	wg.Wait()
	snap := h.Snapshot()
	if snap.Count != 2500 {
		t.Errorf("Count = %d, want 2500", snap.Count)
	}
}

func TestEscapeLabelValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"plain", `hello`, `hello`},
		{"quote", `a"b`, `a\"b`},
		{"backslash", `a\b`, `a\\b`},
		{"newline", "line\nbreak", `line\nbreak`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := escapeLabelValue(tc.in); got != tc.want {
				t.Errorf("escapeLabelValue(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
