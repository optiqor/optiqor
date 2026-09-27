package sandbox

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/cost"
)

// TestPerf_AnalyzeP95UnderBudget asserts the Phase-2 p95 < 3s budget
// on /v1/analyze for the 30-detector demo chart. In-process so a CI
// failure indicts the engine, not the loopback socket. OPTIQOR_PERF=1
// raises iterations for local capacity work.
func TestPerf_AnalyzeP95UnderBudget(t *testing.T) {
	iterations := 30
	if !testing.Short() && os.Getenv("OPTIQOR_PERF") == "1" {
		iterations = 200
	}

	body, err := os.ReadFile(filepath.Join("..", "..", "..", "cmd", "optiqor", "demo", "values.yaml"))
	if err != nil {
		t.Skipf("demo chart not available (CLI repo missing?): %v", err)
	}

	h := &Handler{
		Store:  NewInMemoryStore(),
		Pricer: cost.NewStaticPricer(),
		Region: "us-east-1",
		Now:    func() time.Time { return time.Now().UTC() },
	}

	// Discard the cold-cache request so p95 reflects steady-state.
	if !runOne(t, h, body, http.StatusOK) {
		t.Fatal("warm-up request failed")
	}

	durations := make([]time.Duration, 0, iterations)
	for i := 0; i < iterations; i++ {
		start := time.Now()
		if !runOne(t, h, body, http.StatusOK) {
			t.Fatalf("iteration %d failed", i)
		}
		durations = append(durations, time.Since(start))
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 := durations[len(durations)*50/100]
	p95 := durations[len(durations)*95/100]
	p99 := durations[len(durations)*99/100]

	const budget = 3 * time.Second
	t.Logf("analyze latency over %d iterations: p50=%v p95=%v p99=%v (budget %v)",
		iterations, p50, p95, p99, budget)

	if p95 > budget {
		t.Errorf("p95 %v exceeds Phase-2 budget %v", p95, budget)
	}
}

func runOne(t *testing.T, h *Handler, body []byte, wantStatus int) bool {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/v1/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-yaml")
	rec := httptest.NewRecorder()
	h.Analyze(rec, req)
	if rec.Code != wantStatus {
		t.Errorf("status: got %d want %d (body=%s)", rec.Code, wantStatus, rec.Body.String())
		return false
	}
	return true
}
