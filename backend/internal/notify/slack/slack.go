// Package slack ships the Phase-5 Slack notifications: cost-spike
// alerts, daily digest, weekly report. Authentication is the simple
// incoming-webhook model — tenant pastes a URL on the dashboard
// settings page, backend stores it KMS-encrypted in slack_installations.
// Full OAuth + slash commands ship in Phase 7 (Slack App approval
// can take weeks).
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MaxBody caps the payload Slack accepts on incoming webhooks (40 KiB
// per their docs; we stay safely under). Defends against a runaway
// findings list overflowing the post.
const MaxBody = 32 * 1024

// Payload is the subset of Slack's Block Kit we use. Webhook posts
// take this shape; the same JSON works against #channel hooks and
// app-level webhooks alike.
type Payload struct {
	Text   string  `json:"text"`
	Blocks []Block `json:"blocks,omitempty"`
}

type Block struct {
	Type string `json:"type"`
	Text *Text  `json:"text,omitempty"`
}

type Text struct {
	Type string `json:"type"` // "mrkdwn" | "plain_text"
	Text string `json:"text"`
}

// Client posts to a Slack incoming webhook URL. One Client per tenant
// — the webhook URL is the tenant identifier as far as Slack is
// concerned. Safe for concurrent use.
type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// PostWebhook sends payload to webhookURL. Validates the URL is a
// real Slack webhook before any HTTP traffic so a typo at settings
// time fails fast.
func (c *Client) PostWebhook(ctx context.Context, webhookURL string, p Payload) error {
	if err := ValidateWebhookURL(webhookURL); err != nil {
		return err
	}
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("slack: marshal: %w", err)
	}
	if len(body) > MaxBody {
		return fmt.Errorf("slack: payload %d bytes exceeds %d cap", len(body), MaxBody)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		// Slack returns a short body like "invalid_token" or "channel_not_found".
		short, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("slack: %d: %s", resp.StatusCode, strings.TrimSpace(string(short)))
	}
	return nil
}

// ValidateWebhookURL accepts only https://hooks.slack.com/services/
// URLs. Rejects anything else so a CSRF-style trick can't aim the
// poster at an attacker-controlled host with the same payload shape.
func ValidateWebhookURL(raw string) error {
	if raw == "" {
		return errors.New("slack: empty webhook URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("slack: parse URL: %w", err)
	}
	if u.Scheme != "https" {
		return errors.New("slack: webhook URL must be https")
	}
	if !strings.EqualFold(u.Host, "hooks.slack.com") {
		return errors.New("slack: webhook URL host must be hooks.slack.com")
	}
	if !strings.HasPrefix(u.Path, "/services/") {
		return errors.New("slack: webhook URL must start /services/")
	}
	return nil
}
