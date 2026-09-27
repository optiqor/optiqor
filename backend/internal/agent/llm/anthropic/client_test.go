package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/agent"
)

func TestNew_EmptyKey_Errors(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("want ErrNoAPIKey, got %v", err)
	}
}

func TestGenerate_SendsCacheControlOnCachedBlockOnly(t *testing.T) {
	var captured messagesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key: got %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != apiVersion {
			t.Errorf("anthropic-version: got %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("unmarshal request: %v", err)
		}
		_, _ = w.Write([]byte(`{
			"content": [{"type":"text","text":"EXPLANATION:\nok\nDIFF:\n--- a\n+++ b"}],
			"model": "claude-sonnet-4-6-20260101",
			"usage": {"input_tokens": 50, "output_tokens": 20, "cache_creation_input_tokens": 200, "cache_read_input_tokens": 800}
		}`))
	}))
	t.Cleanup(srv.Close)

	c, err := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := c.Generate(context.Background(), agent.LLMRequest{
		CachedSystem: "stable prefix used by every call",
		System:       "per-call hint",
		User:         "user content",
		Model:        "claude-sonnet-4-6",
		MaxTokens:    1024,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if len(captured.System) != 2 {
		t.Fatalf("want 2 system blocks, got %d", len(captured.System))
	}
	if captured.System[0].CacheControl == nil || captured.System[0].CacheControl.Type != "ephemeral" {
		t.Errorf("cached block missing cache_control: %+v", captured.System[0])
	}
	if captured.System[1].CacheControl != nil {
		t.Errorf("tail block must not be cached: %+v", captured.System[1])
	}
	if !strings.Contains(resp.Text, "EXPLANATION:") || !strings.Contains(resp.Text, "DIFF:") {
		t.Errorf("response body missing markers: %q", resp.Text)
	}
	if resp.CacheReadTokens != 800 || resp.CacheCreateTokens != 200 {
		t.Errorf("cache tokens: read=%d create=%d", resp.CacheReadTokens, resp.CacheCreateTokens)
	}
	if resp.CostUSDCents <= 0 {
		t.Errorf("want positive cost, got %d", resp.CostUSDCents)
	}
}

func TestGenerate_OmitsCacheControlWhenNoCachedSystem(t *testing.T) {
	var captured messagesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
	}))
	t.Cleanup(srv.Close)

	c, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, _ = c.Generate(context.Background(), agent.LLMRequest{User: "hi", MaxTokens: 16})

	for _, blk := range captured.System {
		if blk.CacheControl != nil {
			t.Fatalf("unexpected cache_control: %+v", blk)
		}
	}
}

func TestGenerate_PropagatesAPIErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad model"}}`))
	}))
	t.Cleanup(srv.Close)

	c, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := c.Generate(context.Background(), agent.LLMRequest{User: "hi", MaxTokens: 16})
	if err == nil || !strings.Contains(err.Error(), "bad model") {
		t.Fatalf("want propagated message, got %v", err)
	}
}

func TestCost_ChargesCacheReadAt10Percent(t *testing.T) {
	c := cost("claude-sonnet-4-6", usage{InputTokens: 1_000_000, OutputTokens: 0, CacheReadInputTokens: 1_000_000})
	// 300 cents (regular) + 30 cents (cache read at 10%) = 330.
	if c != 330 {
		t.Errorf("want 330 cents, got %d", c)
	}
}
