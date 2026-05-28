// Package prom is the agent-side Prometheus scraper. The agent issues
// a small canonical PromQL set every snapshot interval against the
// customer's in-cluster Prometheus (any conformant /api/v1/query
// endpoint — kube-prometheus-stack, Mimir, Cortex, VictoriaMetrics)
// and ships the resulting samples in the AgentSnapshot.
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
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is the seam to a Prometheus instant-query endpoint. Production
// wires HTTPClient; tests inject a deterministic InMemoryClient that
// returns canned samples per query string.
type Client interface {
	Query(ctx context.Context, promql string, at time.Time) ([]Sample, error)
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

// HTTPClient calls /api/v1/query on a Prometheus-shaped HTTP backend.
// Defaults: 5s timeout per query, single retry on 5xx, JSON body
// capped at 8 MiB. The defaults match what kube-prometheus-stack
// surfaces over the cluster-internal Service.
type HTTPClient struct {
	BaseURL      string // e.g. http://prometheus-operated.monitoring:9090
	HTTP         *http.Client
	MaxBodyBytes int64
}

// NewHTTPClient validates the URL shape and applies safe defaults.
// Empty BaseURL is a programmer bug; callers gate construction on the
// OPTIQOR_PROMETHEUS_URL env var being non-empty.
func NewHTTPClient(baseURL string) (*HTTPClient, error) {
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
	return &HTTPClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP: &http.Client{
			Timeout: 5 * time.Second,
		},
		MaxBodyBytes: 8 << 20,
	}, nil
}

type instantResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string          `json:"resultType"`
		Result     []instantResult `json:"result"`
	} `json:"data"`
	Error string `json:"error"`
}

type instantResult struct {
	Metric map[string]string `json:"metric"`
	Value  []json.RawMessage `json:"value"` // [<unix-seconds float>, "<scalar string>"]
}

// Query executes promql at time at and returns the resulting samples.
// A 4xx response wraps an Err with the API's error string; a 5xx
// triggers one retry then bubbles up. Empty result is (nil, nil) so
// the caller never has to nil-check.
func (c *HTTPClient) Query(ctx context.Context, promql string, at time.Time) ([]Sample, error) {
	if c == nil || c.HTTP == nil {
		return nil, errors.New("prom: nil HTTPClient")
	}
	q := url.Values{}
	q.Set("query", promql)
	q.Set("time", strconv.FormatFloat(float64(at.Unix())+float64(at.Nanosecond())/1e9, 'f', -1, 64))

	endpoint := c.BaseURL + "/api/v1/query?" + q.Encode()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		samples, err := c.doOnce(ctx, endpoint)
		if err == nil {
			return samples, nil
		}
		lastErr = err
		var hErr httpError
		if !errors.As(err, &hErr) || hErr.status < 500 {
			break
		}
	}
	return nil, lastErr
}

func (c *HTTPClient) doOnce(ctx context.Context, endpoint string) ([]Sample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("prom: new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("prom: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, httpError{status: resp.StatusCode, body: strings.TrimSpace(string(body))}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, c.MaxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("prom: read body: %w", err)
	}
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
		var tsSec float64
		if err := json.Unmarshal(r.Value[0], &tsSec); err != nil {
			continue
		}
		var raw string
		if err := json.Unmarshal(r.Value[1], &raw); err != nil {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		out = append(out, Sample{
			Labels: r.Metric,
			Value:  v,
			At:     time.Unix(int64(tsSec), int64((tsSec-float64(int64(tsSec)))*1e9)).UTC(),
		})
	}
	return out, nil
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
