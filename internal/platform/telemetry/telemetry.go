// Phase 1: in-house Prometheus text-format exposition (counters +
// histograms only) and a no-op tracer. client_golang and the OTel SDK
// land in Phase 6 — call sites go through these interfaces so the swap
// is local to this package.
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

type Counter interface {
	Inc()
	Add(delta float64)
	Value() float64
}

// Histogram surface is bucket-counts only in Phase 1; quantile
// estimation arrives with client_golang.
type Histogram interface {
	Observe(v float64)
	Snapshot() HistogramSnapshot
}

// HistogramSnapshot mirrors Prometheus exposition: Boundaries are le
// upper bounds; the +Inf bucket is appended by the renderer.
type HistogramSnapshot struct {
	Boundaries []float64
	Counts     []uint64
	Sum        float64
	Count      uint64
}

// Registry is one per process, shared by pointer.
type Registry struct {
	mu         sync.Mutex
	counters   map[string]*counter
	histograms map[string]*histogram
}

func NewRegistry() *Registry {
	return &Registry{
		counters:   map[string]*counter{},
		histograms: map[string]*histogram{},
	}
}

// NewCounter is idempotent on (name, labels) so cmd/* boot can be
// re-entrant under fork/exec patterns.
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

// NewHistogram is idempotent on (name, labels). Boundaries must be
// strictly ascending; the +Inf bucket is appended internally.
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

// Handler emits the Prometheus text-format exposition. Mount on /metrics.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = r.WriteText(w)
	})
}

// WriteText exposes the rendering for tests to assert without an HTTP
// roundtrip.
func (r *Registry) WriteText(w io.Writer) error {
	r.mu.Lock()
	defer r.mu.Unlock()

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
		// Buckets are cumulative per Prometheus convention.
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

// Tracer is what domain code calls; the OTel SDK adapter lands in
// Phase 6.
type Tracer interface {
	Start(ctx context.Context, name string) (context.Context, Span)
}

type Span interface {
	End()
	SetAttribute(key string, value any)
	RecordError(err error)
}

// NoopTracer is the safe default; replaced via DI at boot.
func NoopTracer() Tracer { return noopTracer{} }

type noopTracer struct{}

func (noopTracer) Start(ctx context.Context, _ string) (context.Context, Span) {
	return ctx, noopSpan{}
}

type noopSpan struct{}

func (noopSpan) End()                         {}
func (noopSpan) SetAttribute(_ string, _ any) {}
func (noopSpan) RecordError(_ error)          {}

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
	counts     []uint64 // len(boundaries)+1; last is the +Inf overflow bucket
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
	// Prometheus exposition escapes: backslash, double-quote, newline.
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
