// Package anthropic is the production LLMClient adapter. It speaks
// /v1/messages with cache_control: ephemeral on the cached prefix so
// the steady-state cost honours the Year-1 50% hit-rate target.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/optiqor/optiqor/internal/agent"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"
	defaultModel   = "claude-sonnet-4-6"
)

// Config wires the adapter. APIKey is mandatory; everything else has a
// safe default. HTTPClient defaults to a 60s-deadline client so a
// hanging upstream cannot exhaust an api goroutine.
type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	UserAgent  string
}

type Client struct {
	cfg Config
}

// ErrNoAPIKey is returned by New when Config.APIKey is empty so the
// caller fails closed rather than booting with a useless adapter.
var ErrNoAPIKey = errors.New("anthropic: empty API key")

func New(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, ErrNoAPIKey
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "optiqor-backend/1.0"
	}
	return &Client{cfg: cfg}, nil
}

var _ agent.LLMClient = (*Client)(nil)

type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type cacheControl struct {
	Type string `json:"type"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	System    []systemBlock `json:"system,omitempty"`
	Messages  []message     `json:"messages"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type messagesResponse struct {
	Content []contentItem `json:"content"`
	Model   string        `json:"model"`
	Usage   usage         `json:"usage"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Type  string   `json:"type"`
	Error apiError `json:"error"`
}

// Generate issues one /v1/messages call. CachedSystem is the only
// block tagged with cache_control so callers cannot accidentally bust
// the cache by varying the rest of the prompt.
func (c *Client) Generate(ctx context.Context, req agent.LLMRequest) (agent.LLMResponse, error) {
	if req.User == "" {
		return agent.LLMResponse{}, errors.New("anthropic: empty user prompt")
	}

	model := normaliseModel(req.Model)
	body := messagesRequest{
		Model:     model,
		MaxTokens: req.MaxTokens,
		Messages:  []message{{Role: "user", Content: req.User}},
	}
	if req.CachedSystem != "" {
		body.System = append(body.System, systemBlock{
			Type:         "text",
			Text:         req.CachedSystem,
			CacheControl: &cacheControl{Type: "ephemeral"},
		})
	}
	if req.System != "" {
		body.System = append(body.System, systemBlock{Type: "text", Text: req.System})
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return agent.LLMResponse{}, fmt.Errorf("anthropic: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return agent.LLMResponse{}, fmt.Errorf("anthropic: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("anthropic-version", apiVersion)
	httpReq.Header.Set("x-api-key", c.cfg.APIKey)
	httpReq.Header.Set("User-Agent", c.cfg.UserAgent)

	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return agent.LLMResponse{}, fmt.Errorf("anthropic: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return agent.LLMResponse{}, fmt.Errorf("anthropic: read: %w", err)
	}

	if resp.StatusCode/100 != 2 {
		var env errorEnvelope
		if json.Unmarshal(respBody, &env) == nil && env.Error.Message != "" {
			return agent.LLMResponse{}, fmt.Errorf("anthropic: %d %s: %s", resp.StatusCode, env.Error.Type, env.Error.Message)
		}
		return agent.LLMResponse{}, fmt.Errorf("anthropic: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	var parsed messagesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return agent.LLMResponse{}, fmt.Errorf("anthropic: unmarshal: %w", err)
	}

	text := joinText(parsed.Content)
	return agent.LLMResponse{
		Text:              text,
		InputTokens:       parsed.Usage.InputTokens,
		OutputTokens:      parsed.Usage.OutputTokens,
		CacheReadTokens:   parsed.Usage.CacheReadInputTokens,
		CacheCreateTokens: parsed.Usage.CacheCreationInputTokens,
		CostUSDCents:      cost(parsed.Model, parsed.Usage),
		Model:             parsed.Model,
	}, nil
}

func joinText(items []contentItem) string {
	var b strings.Builder
	for _, it := range items {
		if it.Type == "text" {
			b.WriteString(it.Text)
		}
	}
	return b.String()
}

// cost mirrors agent.perMillionCents — kept local so the adapter
// produces a CostUSDCents that callers can record even when the agent
// package upgrades pricing tables. Cache reads are billed at 10% of
// input, cache writes at 125% per Anthropic's published rates.
func cost(model string, u usage) int64 {
	in, out := perMillion(model)
	regularIn := int64(u.InputTokens)
	cents := (regularIn*in + int64(u.OutputTokens)*out) / 1_000_000
	cents += (int64(u.CacheReadInputTokens) * in) / (1_000_000 * 10)
	cents += (int64(u.CacheCreationInputTokens) * in * 5) / (1_000_000 * 4)
	if cents < 1 {
		return 1
	}
	return cents
}

func perMillion(model string) (in, out int64) {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "haiku"):
		return 100, 500
	case strings.Contains(m, "opus"):
		return 1500, 7500
	}
	return 300, 1500
}

func normaliseModel(m string) string {
	if m == "" {
		return defaultModel
	}
	return m
}
