package sandbox

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/cost"
)

const exampleChart = `api:
  replicas: 3
  resources:
    requests: {cpu: 500m, memory: 256Mi}
    limits:   {cpu: 1, memory: 512Mi}
  image: nginx:1.25
worker:
  resources:
    requests: {cpu: 200m, memory: 128Mi}
`

func newHandler() *Handler {
	return &Handler{
		Store:  NewInMemoryStore(),
		Pricer: cost.NewStaticPricer(),
		Region: "us-east-1",
		Now:    func() time.Time { return time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC) },
	}
}

func TestAnalyze_RejectsNonPost(t *testing.T) {
	h := newHandler()
	req := httptest.NewRequest(http.MethodGet, "/v1/analyze", http.NoBody)
	w := httptest.NewRecorder()
	h.Analyze(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("code = %d", w.Code)
	}
}

func TestAnalyze_HappyPath(t *testing.T) {
	h := newHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(exampleChart))
	w := httptest.NewRecorder()
	h.Analyze(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	var resp AnalyzeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v\nbody:%s", err, w.Body.String())
	}
	if resp.AccuracyDisclosure != AccuracyDisclosure {
		t.Errorf("disclosure mismatch: %q", resp.AccuracyDisclosure)
	}
	if resp.Workloads != 2 {
		t.Errorf("workloads = %d, want 2", resp.Workloads)
	}
	if resp.ShareHash == "" {
		t.Error("share_hash empty")
	}
	// Default handler (no PublicBaseURL) derives from request host —
	// httptest defaults to example.com. The /r/<hash> suffix is the
	// stable part; we assert that and not the origin.
	if !strings.Contains(resp.ShareURL, "/r/"+resp.ShareHash) {
		t.Errorf("share_url = %q, missing /r/<hash> suffix", resp.ShareURL)
	}
	if len(resp.CostFindings)+len(resp.SecurityFindingsBonus) != len(resp.Findings) {
		t.Errorf("split mismatch: cost=%d security=%d findings=%d",
			len(resp.CostFindings), len(resp.SecurityFindingsBonus), len(resp.Findings))
	}
	if resp.AnnualSavingsUSD != resp.MonthlySavingsUSD*12 {
		t.Errorf("annual != monthly*12: %v vs %v", resp.AnnualSavingsUSD, resp.MonthlySavingsUSD)
	}
}

func TestAnalyze_ShareURL_DerivedFromRequestInDev(t *testing.T) {
	h := newHandler() // no PublicBaseURL
	req := httptest.NewRequest(http.MethodPost, "http://localhost:3000/v1/analyze", strings.NewReader(exampleChart))
	req.Host = "localhost:3000"
	w := httptest.NewRecorder()
	h.Analyze(w, req)
	var resp AnalyzeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.HasPrefix(resp.ShareURL, "http://localhost:3000/r/") {
		t.Errorf("share_url should derive from request: got %q", resp.ShareURL)
	}
}

func TestAnalyze_ShareURL_HonoursPublicBaseURL(t *testing.T) {
	h := newHandler()
	h.PublicBaseURL = "https://optiqor.dev"
	req := httptest.NewRequest(http.MethodPost, "http://localhost:3000/v1/analyze", strings.NewReader(exampleChart))
	w := httptest.NewRecorder()
	h.Analyze(w, req)
	var resp AnalyzeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.HasPrefix(resp.ShareURL, "https://optiqor.dev/r/") {
		t.Errorf("share_url should honour configured PublicBaseURL: got %q", resp.ShareURL)
	}
}

func TestAnalyze_ShareURL_HonoursXForwardedProto(t *testing.T) {
	h := newHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(exampleChart))
	req.Host = "optiqor.dev"
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	h.Analyze(w, req)
	var resp AnalyzeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.HasPrefix(resp.ShareURL, "https://optiqor.dev/r/") {
		t.Errorf("X-Forwarded-Proto should drive scheme: got %q", resp.ShareURL)
	}
}

func TestAnalyze_BadYAML_400(t *testing.T) {
	h := newHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(":\n  - not: [valid"))
	w := httptest.NewRecorder()
	h.Analyze(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestAnalyze_OversizedBody_413(t *testing.T) {
	h := newHandler()
	big := strings.Repeat("a", int(MaxBodyBytes)+1)
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(big))
	w := httptest.NewRecorder()
	h.Analyze(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("code = %d", w.Code)
	}
}

func TestAnalyze_StoresShareEntry_HTMLByDefault(t *testing.T) {
	h := newHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(exampleChart))
	w := httptest.NewRecorder()
	h.Analyze(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("analyze failed: %s", w.Body.String())
	}
	var resp AnalyzeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	mux := http.NewServeMux()
	h.Mount(mux)

	// Default Accept → HTML report rendered via pkg/htmlrender.
	getReq := httptest.NewRequest(http.MethodGet, "/r/"+resp.ShareHash, http.NoBody)
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("share GET code = %d, body = %s", getW.Code, getW.Body.String())
	}
	if ct := getW.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("default content-type = %q, want text/html", ct)
	}
	if !strings.Contains(getW.Body.String(), "<!doctype html>") {
		t.Errorf("default share render is not HTML:\n%s", getW.Body.String()[:200])
	}
	if !strings.Contains(getW.Body.String(), "Sandbox accuracy: ±40%") {
		t.Errorf("HTML share missing accuracy disclosure")
	}
}

func TestAnalyze_StoresShareEntry_JSONOnAccept(t *testing.T) {
	h := newHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(exampleChart))
	w := httptest.NewRecorder()
	h.Analyze(w, req)
	var resp AnalyzeResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)

	mux := http.NewServeMux()
	h.Mount(mux)

	getReq := httptest.NewRequest(http.MethodGet, "/r/"+resp.ShareHash, http.NoBody)
	getReq.Header.Set("Accept", "application/json")
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("share GET code = %d", getW.Code)
	}
	if !strings.Contains(getW.Body.String(), "accuracy_disclosure") {
		t.Errorf("Accept:application/json branch missing disclosure key")
	}

	// ?format=json query also opts into JSON.
	getReq2 := httptest.NewRequest(http.MethodGet, "/r/"+resp.ShareHash+"?format=json", http.NoBody)
	getW2 := httptest.NewRecorder()
	mux.ServeHTTP(getW2, getReq2)
	if !strings.Contains(getW2.Body.String(), "accuracy_disclosure") {
		t.Errorf("?format=json branch missing disclosure key")
	}
}

func TestShare_404OnMissing(t *testing.T) {
	h := newHandler()
	mux := http.NewServeMux()
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/r/deadbeef", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d", w.Code)
	}
}

func TestShare_RespectsExpiry(t *testing.T) {
	store := NewInMemoryStore()
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now.Add(31 * 24 * time.Hour) } // simulate "tomorrow + 30 days"
	_ = store.Put(context.Background(), SharedAnalysis{
		Hash:      "x",
		Body:      []byte("{}"),
		MediaType: "application/json",
		ExpiresAt: now.Add(1 * time.Hour),
	})
	if _, err := store.Get(context.Background(), "x"); err == nil {
		t.Error("expected ErrNotFound for expired entry")
	}
}

func TestAnalyze_DisclosureAlwaysPresent(t *testing.T) {
	h := newHandler()
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(exampleChart))
	w := httptest.NewRecorder()
	h.Analyze(w, req)
	body, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(body), "±40%") {
		t.Errorf("response missing accuracy disclosure:\n%s", body)
	}
}

func TestHashBytes_StableAcrossCalls(t *testing.T) {
	a := hashBytes([]byte("hello"))
	b := hashBytes([]byte("hello"))
	if a != b {
		t.Errorf("hash non-deterministic: %s vs %s", a, b)
	}
	if len(a) != 24 { // 12 bytes hex
		t.Errorf("hash length = %d, want 24", len(a))
	}
}
