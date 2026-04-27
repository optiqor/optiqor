// Package telemetry provides metrics and tracing primitives for Sevro.
//
// Phase 1 ships a minimal in-house Prometheus text-format exposition
// (counters + histograms only) and a no-op OpenTelemetry tracer
// interface. The full client_golang and OTel SDK swap in cleanly when
// the observability stack lands in Phase 6 — every call site goes
// through the interfaces here so the swap is local to this package.
package telemetry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Counter is a monotonically-increasing scalar.
type Counter interface {
	Inc()
	Add(delta float64)
	Value() float64
}

// Histogram approximates a distribution. Phase 1 surface is bucket-counts
// only; quantile estimation arrives with client_golang.
type Histogram interface {
	Observe(v float64)
	// Snapshot returns a deterministic copy of bucket counts in ascending
	// upper-bound order, plus the running sum and count.
	Snapshot() HistogramSnapshot
}

// HistogramSnapshot is a point-in-time view used by the Prometheus
// text-format renderer. Boundaries are upper bounds (le, in Prometheus
// terminology); the final +Inf bucket is appended automatically.
type HistogramSnapshot struct {
	Boundaries []float64
	Counts     []uint64
	Sum        float64
	Count      uint64
}

// Registry holds the metrics this process exports. Construct one per
// process; share by pointer.
type Registry struct {
	mu         sync.Mutex
	counters   map[string]*counter
	histograms map[string]*histogram
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:   map[string]*counter{},
		histograms: map[string]*histogram{},
	}
}

// NewCounter registers (or returns an existing) counter. Re-registration
// with the same name+labels is a no-op so cmd/* boot can be re-entrant.
func (r *Registry) NewCounter(name, help string, labels map[string]string) Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := metricKey(name, labels)
	if c, ok := r.counters[key]; ok {
		return c
	}
	c := &counter{name: name, help: help, labels: labels}
	r.counters[key] = c
	return c
}

// NewHistogram registers (or returns an existing) histogram with the
// given upper-bound bucket boundaries. Boundaries must be ascending;
// the final +Inf bucket is appended internally.
func (r *Registry) NewHistogram(name, help string, labels map[string]string, boundaries []float64) Histogram {
	if !ascending(boundaries) {
		panic("telemetry: histogram boundaries must be strictly ascending")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := metricKey(name, labels)
	if h, ok := r.histograms[key]; ok {
		return h
	}
	h := &histogram{
		name:       name,
		help:       help,
		labels:     labels,
		boundaries: append([]float64(nil), boundaries...),
		counts:     make([]uint64, len(boundaries)+1),
	}
	r.histograms[key] = h
	return h
}

// Handler returns an http.Handler that emits the Prometheus
// text-format exposition. Mount on /metrics.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = r.WriteText(w)
	})
}

// WriteText writes the exposition to w. Exposed so tests can assert on
// raw output without a server roundtrip.
func (r *Registry) WriteText(w io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Counters first, deterministic order.
	cnames := make([]string, 0, len(r.counters))
	for k := range r.counters {
		cnames = append(cnames, k)
	}
	sort.Strings(cnames)
	for _, k := range cnames {
		c := r.counters[k]
		if c.help != "" {
			_, _ = fmt.Fprintf(w, "# HELP %s %s\n", c.name, c.help)
		}
		_, _ = fmt.Fprintf(w, "# TYPE %s counter\n", c.name)
		_, _ = fmt.Fprintf(w, "%s%s %g\n", c.name, formatLabels(c.labels), c.Value())
	}

	hnames := make([]string, 0, len(r.histograms))
	for k := range r.histograms {
		hnames = append(hnames, k)
	}
	sort.Strings(hnames)
	for _, k := range hnames {
		h := r.histograms[k]
		if h.help != "" {
			_, _ = fmt.Fprintf(w, "# HELP %s %s\n", h.name, h.help)
		}
		_, _ = fmt.Fprintf(w, "# TYPE %s histogram\n", h.name)
		snap := h.Snapshot()
		// Cumulative counts per Prometheus convention.
		var cum uint64
		for i, b := range snap.Boundaries {
			cum += snap.Counts[i]
			labels := mergeLabels(h.labels, "le", fmt.Sprintf("%g", b))
			_, _ = fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, formatLabels(labels), cum)
		}
		cum += snap.Counts[len(snap.Counts)-1]
		labels := mergeLabels(h.labels, "le", "+Inf")
		_, _ = fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, formatLabels(labels), cum)
		_, _ = fmt.Fprintf(w, "%s_sum%s %g\n", h.name, formatLabels(h.labels), snap.Sum)
		_, _ = fmt.Fprintf(w, "%s_count%s %d\n", h.name, formatLabels(h.labels), snap.Count)
	}
	return nil
}

// Tracer is the contract domain code uses for distributed tracing.
// Phase 1 ships a no-op tracer; the OTel SDK adapter lands with the
// observability rollout (Phase 6).
type Tracer interface {
	Start(ctx context.Context, name string) (context.Context, Span)
}

// Span represents an in-flight trace span.
type Span interface {
	End()
	SetAttribute(key string, value any)
	RecordError(err error)
}

// NoopTracer returns a tracer that does nothing. Safe default; callers
// can replace via DI when a real tracer is configured at boot.
func NoopTracer() Tracer { return noopTracer{} }

type noopTracer struct{}

func (noopTracer) Start(ctx context.Context, _ string) (context.Context, Span) {
	return ctx, noopSpan{}
}

type noopSpan struct{}

func (noopSpan) End()                         {}
func (noopSpan) SetAttribute(_ string, _ any) {}
func (noopSpan) RecordError(_ error)          {}

// ---- internals ----

type counter struct {
	mu     sync.Mutex
	name   string
	help   string
	labels map[string]string
	value  float64
}

func (c *counter) Inc()              { c.Add(1) }
func (c *counter) Add(delta float64) { c.mu.Lock(); c.value += delta; c.mu.Unlock() }
func (c *counter) Value() float64    { c.mu.Lock(); defer c.mu.Unlock(); return c.value }

type histogram struct {
	mu         sync.Mutex
	name       string
	help       string
	labels     map[string]string
	boundaries []float64
	counts     []uint64 // len == len(boundaries) + 1; last is the overflow (+Inf) bucket
	sum        float64
	total      uint64
}

func (h *histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += v
	h.total++
	for i, b := range h.boundaries {
		if v <= b {
			h.counts[i]++
			return
		}
	}
	h.counts[len(h.counts)-1]++
}

func (h *histogram) Snapshot() HistogramSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return HistogramSnapshot{
		Boundaries: append([]float64(nil), h.boundaries...),
		Counts:     append([]uint64(nil), h.counts...),
		Sum:        h.sum,
		Count:      h.total,
	}
}

func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(labels[k])
	}
	b.WriteString("}")
	return b.String()
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `%s="%s"`, k, escapeLabelValue(labels[k]))
	}
	b.WriteString("}")
	return b.String()
}

func mergeLabels(base map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(base)+1)
	for kk, vv := range base {
		out[kk] = vv
	}
	out[k] = v
	return out
}

func escapeLabelValue(s string) string {
	// Per Prometheus exposition: backslash, double-quote, newline.
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(s)
}

func ascending(xs []float64) bool {
	for i := 1; i < len(xs); i++ {
		if xs[i] <= xs[i-1] {
			return false
		}
	}
	return true
}
