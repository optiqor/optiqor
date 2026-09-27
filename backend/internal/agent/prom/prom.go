// Package prom is the agent-side Prometheus query client. The agent
// issues PromQL against the customer's in-cluster Prometheus (any
// conformant /api/v1/{query,query_range} endpoint — kube-prometheus-stack,
// Mimir, Cortex, VictoriaMetrics, Thanos) and ships the resulting
// samples in the AgentSnapshot.
//
// The SaaS-side OOMKilled reader at internal/cost/oomkilled uses a
// similar QueryClient seam but runs as a one-shot query against a
// remote Prom over CUR-style backfills. This package is the recurring
// live-scrape loop counterpart.
package prom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is the seam to a Prometheus query endpoint. Production wires
// HTTPClient; tests inject a deterministic InMemoryClient. The two
// methods cover the two query shapes the canonical query set uses —
// Query for the current snapshot, QueryRange for the 30-day percentile
// roll-ups the statistical sizing engine consumes.
type Client interface {
	Query(ctx context.Context, promql string, at time.Time) ([]Sample, error)
	QueryRange(ctx context.Context, promql string, r Range) ([]Series, error)
}

// Sample is a single label-set + scalar reading. Workload identity is
// resolved by the caller from the labels — the scraper doesn't
// hard-bind label keys so the same client works against kubelet
// cAdvisor labels, kube-state-metrics labels, and Karpenter labels
// without retuning.
type Sample struct {
	Labels map[string]string
	Value  float64
	At     time.Time
}

// Range is the input to QueryRange. Step bounds the resolution per the
// canonical query set: instant snapshots use 1m, 30d roll-ups use 1h
// (720 points per series).
type Range struct {
	Start time.Time
	End   time.Time
	Step  time.Duration
}

// Series is one label-set's points over a Range. Empty Points is the
// "no data" case Prometheus returns — caller treats it as "skip", not
// "error".
type Series struct {
	Labels map[string]string
	Points []Point
}

type Point struct {
	At    time.Time
	Value float64
}

// Observer reports per-query latency + outcome. nil-safe so callers
// that don't wire telemetry stay slim. Production binds it to
// optiqor_agent_prom_query_seconds + optiqor_agent_prom_errors_total
// (cmd/agent/main.go).
type Observer interface {
	ObserveQuery(kind string, status string, latency time.Duration)
}

// HTTPClient calls /api/v1/{query,query_range} on a Prometheus-shaped
// HTTP backend. Defaults: 30s timeout per query (range queries against
// 30d windows on remote Mimir easily run past 5s), single retry on 5xx,
// JSON body capped at 64 MiB (matrix responses with 720 points × 200
// containers run ~5-10 MiB). The defaults match what kube-prometheus-stack
// surfaces over the cluster-internal Service.
type HTTPClient struct {
	BaseURL      string
	HTTP         *http.Client
	MaxBodyBytes int64
	Auth         Authenticator
	Obs          Observer
}

// HTTPOption is the functional-option pattern; cmd/agent wires the
// concrete Authenticator + Observer at boot.
type HTTPOption func(*HTTPClient) error

func WithAuth(a Authenticator) HTTPOption {
	return func(c *HTTPClient) error { c.Auth = a; return nil }
}

func WithObserver(o Observer) HTTPOption {
	return func(c *HTTPClient) error { c.Obs = o; return nil }
}

func WithTimeout(d time.Duration) HTTPOption {
	return func(c *HTTPClient) error {
		if d <= 0 {
			return errors.New("prom: timeout must be positive")
		}
		c.HTTP.Timeout = d
		return nil
	}
}

func WithMaxBodyBytes(n int64) HTTPOption {
	return func(c *HTTPClient) error {
		if n <= 0 {
			return errors.New("prom: MaxBodyBytes must be positive")
		}
		c.MaxBodyBytes = n
		return nil
	}
}

// NewHTTPClient validates the URL shape and applies safe defaults.
// Empty BaseURL is a programmer bug; callers gate construction on the
// OPTIQOR_PROMETHEUS_URL env var being non-empty. Pass WithAuth(...)
// when the customer Prometheus is gated (the common case in prod).
func NewHTTPClient(baseURL string, opts ...HTTPOption) (*HTTPClient, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("prom: empty BaseURL")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("prom: parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("prom: scheme %q not supported", u.Scheme)
	}
	c := &HTTPClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				TLSHandshakeTimeout:   5 * time.Second,
				ResponseHeaderTimeout: 25 * time.Second,
				MaxIdleConns:          4,
				IdleConnTimeout:       90 * time.Second,
			},
		},
		MaxBodyBytes: 64 << 20,
		Auth:         noopAuth{},
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// NewHTTPClientWithMTLS combines mTLS transport setup + auth in one
// constructor. Use this instead of NewHTTPClient(WithAuth(...)) when
// AuthMode is mtls — the cert lives on the transport, not the header.
func NewHTTPClientWithMTLS(baseURL string, cfg AuthConfig, opts ...HTTPOption) (*HTTPClient, error) {
	if cfg.Mode != AuthMTLS {
		return nil, fmt.Errorf("prom: NewHTTPClientWithMTLS called with mode=%q", cfg.Mode)
	}
	tlsCfg, err := BuildTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	c, err := NewHTTPClient(baseURL, opts...)
	if err != nil {
		return nil, err
	}
	if tr, ok := c.HTTP.Transport.(*http.Transport); ok {
		tr.TLSClientConfig = tlsCfg
	}
	c.Auth = mtlsAuth{}
	return c, nil
}

type instantResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string          `json:"resultType"`
		Result     []instantResult `json:"result"`
	} `json:"data"`
	Error    string   `json:"error"`
	Warnings []string `json:"warnings,omitempty"`
}

type instantResult struct {
	Metric map[string]string `json:"metric"`
	Value  []json.RawMessage `json:"value"`
}

type rangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string        `json:"resultType"`
		Result     []rangeResult `json:"result"`
	} `json:"data"`
	Error    string   `json:"error"`
	Warnings []string `json:"warnings,omitempty"`
}

type rangeResult struct {
	Metric map[string]string   `json:"metric"`
	Values [][]json.RawMessage `json:"values"`
}

// Query executes promql at time `at` and returns the resulting vector.
// A 4xx response wraps an error with the API's error string; a 5xx
// triggers one retry then bubbles up. Empty result is (nil, nil) so
// the caller never has to nil-check.
func (c *HTTPClient) Query(ctx context.Context, promql string, at time.Time) ([]Sample, error) {
	if c == nil || c.HTTP == nil {
		return nil, errors.New("prom: nil HTTPClient")
	}
	start := time.Now()
	q := url.Values{}
	q.Set("query", promql)
	q.Set("time", formatTime(at))

	endpoint := c.BaseURL + "/api/v1/query?" + q.Encode()
	out, status, err := c.do(ctx, endpoint, parseInstant)
	c.observe("query", status, time.Since(start))
	if err != nil {
		return nil, err
	}
	samples, ok := out.([]Sample)
	if !ok {
		return nil, errors.New("prom: query returned wrong result type")
	}
	return samples, nil
}

// QueryRange executes promql over r and returns one Series per
// label-set. Step = 0 is rejected because Prometheus requires it; the
// caller (canonical query set) pins it per query shape.
func (c *HTTPClient) QueryRange(ctx context.Context, promql string, r Range) ([]Series, error) {
	if c == nil || c.HTTP == nil {
		return nil, errors.New("prom: nil HTTPClient")
	}
	if r.Step <= 0 {
		return nil, errors.New("prom: QueryRange requires positive Step")
	}
	if !r.End.After(r.Start) {
		return nil, errors.New("prom: QueryRange requires End > Start")
	}
	start := time.Now()
	q := url.Values{}
	q.Set("query", promql)
	q.Set("start", formatTime(r.Start))
	q.Set("end", formatTime(r.End))
	q.Set("step", strconv.FormatFloat(r.Step.Seconds(), 'f', -1, 64))

	endpoint := c.BaseURL + "/api/v1/query_range?" + q.Encode()
	series, status, err := c.do(ctx, endpoint, parseRange)
	c.observe("query_range", status, time.Since(start))
	if err != nil {
		return nil, err
	}
	if s, ok := series.([]Series); ok {
		return s, nil
	}
	return nil, errors.New("prom: query_range returned wrong result type")
}

type parser func([]byte) (any, error)

func (c *HTTPClient) do(ctx context.Context, endpoint string, parse parser) (result any, status string, err error) {
	for attempt := 0; attempt < 2; attempt++ {
		out, st, derr := c.doOnce(ctx, endpoint, parse)
		if derr == nil {
			return out, st, nil
		}
		result, status, err = nil, st, derr
		var hErr httpError
		if !errors.As(derr, &hErr) || hErr.status < 500 {
			break
		}
	}
	return result, status, err
}

func (c *HTTPClient) doOnce(ctx context.Context, endpoint string, parse parser) (result any, status string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, "build_error", fmt.Errorf("prom: new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.Auth != nil {
		if err := c.Auth.Apply(req); err != nil {
			return nil, "auth_error", fmt.Errorf("prom: auth: %w", err)
		}
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "transport_error", fmt.Errorf("prom: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, statusBucket(resp.StatusCode), httpError{status: resp.StatusCode, body: strings.TrimSpace(string(body))}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, c.MaxBodyBytes))
	if err != nil {
		return nil, "read_error", fmt.Errorf("prom: read body: %w", err)
	}
	out, err := parse(body)
	if err != nil {
		return nil, "decode_error", err
	}
	return out, "ok", nil
}

func (c *HTTPClient) observe(kind, status string, latency time.Duration) {
	if c.Obs != nil {
		c.Obs.ObserveQuery(kind, status, latency)
	}
}

func parseInstant(body []byte) (any, error) {
	var parsed instantResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("prom: decode: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("prom: api error: %s", parsed.Error)
	}
	out := make([]Sample, 0, len(parsed.Data.Result))
	for _, r := range parsed.Data.Result {
		if len(r.Value) != 2 {
			continue
		}
		at, v, ok := decodePair(r.Value[0], r.Value[1])
		if !ok {
			continue
		}
		out = append(out, Sample{Labels: r.Metric, Value: v, At: at})
	}
	return out, nil
}

func parseRange(body []byte) (any, error) {
	var parsed rangeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("prom: decode: %w", err)
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("prom: api error: %s", parsed.Error)
	}
	if parsed.Data.ResultType != "matrix" {
		return nil, fmt.Errorf("prom: query_range returned resultType=%q, want matrix", parsed.Data.ResultType)
	}
	out := make([]Series, 0, len(parsed.Data.Result))
	for _, r := range parsed.Data.Result {
		series := Series{Labels: r.Metric, Points: make([]Point, 0, len(r.Values))}
		for _, pair := range r.Values {
			if len(pair) != 2 {
				continue
			}
			at, v, ok := decodePair(pair[0], pair[1])
			if !ok {
				continue
			}
			series.Points = append(series.Points, Point{At: at, Value: v})
		}
		out = append(out, series)
	}
	return out, nil
}

func decodePair(tsRaw, valRaw json.RawMessage) (time.Time, float64, bool) {
	var tsSec float64
	if err := json.Unmarshal(tsRaw, &tsSec); err != nil {
		return time.Time{}, 0, false
	}
	var raw string
	if err := json.Unmarshal(valRaw, &raw); err != nil {
		return time.Time{}, 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return time.Time{}, 0, false
	}
	whole := int64(tsSec)
	frac := int64((tsSec - float64(whole)) * 1e9)
	return time.Unix(whole, frac).UTC(), v, true
}

func formatTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.Unix())+float64(t.Nanosecond())/1e9, 'f', -1, 64)
}

// statusBucket coarsens an HTTP status into a short label safe for
// Prometheus cardinality. Bucketed so a runaway 4xx spread across
// dozens of codes can't blow out the metric.
func statusBucket(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "401"
	case status == http.StatusForbidden:
		return "403"
	case status == http.StatusNotFound:
		return "404"
	case status == http.StatusTooManyRequests:
		return "429"
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	default:
		return strconv.Itoa(status)
	}
}

type httpError struct {
	status int
	body   string
}

func (e httpError) Error() string {
	if e.body == "" {
		return fmt.Sprintf("prom: http %d", e.status)
	}
	return fmt.Sprintf("prom: http %d: %s", e.status, e.body)
}
