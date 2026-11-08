// Package share computes a content-addressable hash of an analysis
// and produces an optiqor.dev/r/<hash> URL.
//
// Hard rules (CLAUDE.md):
//   - No telemetry by default. The CLI must never call out unless
//     the user explicitly passes --share.
//   - PII minimisation. We hash a sanitised payload — file paths and
//     user identifiers are stripped before hashing.
package share

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// BaseURL is the public share-page prefix.
const BaseURL = "https://optiqor.dev/r/"

// HashLen of 12 hex chars (48 bits) is collision-safe at Phase 1
// query volumes; full digests stay available via JSON output.
const HashLen = 12

// Sanitise strips environment-specific fields before hashing:
//   - report.source → "(redacted)"
//   - finding.detail capped at 256 chars (details are deterministic
//     per finding so the truncation is safe)
func Sanitise(report any) (any, error) {
	raw, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("share: marshal: %w", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("share: unmarshal: %w", err)
	}
	if _, ok := doc["source"]; ok {
		doc["source"] = "(redacted)"
	}
	if findings, ok := doc["findings"].([]any); ok {
		for _, f := range findings {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			if d, ok := fm["Detail"].(string); ok && len(d) > 256 {
				fm["Detail"] = d[:256] + "…"
			}
		}
	}
	return doc, nil
}

// Hash returns the hex-encoded SHA-256 of the sanitised JSON
// encoding, truncated to HashLen. Stable across runs — relies on
// encoding/json's sorted-key emission.
func Hash(report any) (string, error) {
	sanitised, err := Sanitise(report)
	if err != nil {
		return "", err
	}
	canonical, err := canonicalJSON(sanitised)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])[:HashLen], nil
}

// URL is the canonical share URL: BaseURL + Hash(report).
func URL(report any) (string, error) {
	h, err := Hash(report)
	if err != nil {
		return "", err
	}
	return BaseURL + h, nil
}

// canonicalJSON re-marshals through map[string]any so nested types
// also get encoding/json's sorted-key ordering.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// UploadEndpoint accepts a sanitised analysis blob. Override via
// OPTIQOR_SHARE_URL for self-hosted Optiqor deployments.
const UploadEndpoint = "https://sandbox.optiqor.dev/api/v1/share"

// uploadTimeout keeps tight — the CLI is interactive.
const uploadTimeout = 5 * time.Second

// UploadResult always populates Hash + URL; Posted reports HTTP 2xx.
type UploadResult struct {
	Hash   string
	URL    string
	Posted bool
	Error  string
}

// Upload attempts to POST the sanitised report JSON to endpoint and
// returns the resulting (hash, URL, posted) tuple.
//
// When private is true, the request carries X-Optiqor-Private: 1 so
// the sandbox receiver returns a token-gated URL (Phase 2 backend work).
// The CLI side only sets the header; the token arrives in the response
// body once the receiver is live.
//
// CLI hard rule: this is the only outbound network call optiqor makes,
// and only when the user explicitly passes --share. The function never
// retries; never logs request bodies; never sends anything but the
// sanitised payload.
func Upload(report any, endpoint string, private bool) UploadResult {
	hash, err := Hash(report)
	if err != nil {
		return UploadResult{Error: err.Error()}
	}
	url := BaseURL + hash
	if endpoint == "" {
		endpoint = UploadEndpoint
	}

	sanitised, err := Sanitise(report)
	if err != nil {
		return UploadResult{Hash: hash, URL: url, Error: err.Error()}
	}
	body, err := canonicalJSON(sanitised)
	if err != nil {
		return UploadResult{Hash: hash, URL: url, Error: err.Error()}
	}

	ctx, cancel := context.WithTimeout(context.Background(), uploadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return UploadResult{Hash: hash, URL: url, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Optiqor-Hash", hash)
	req.Header.Set("User-Agent", "optiqor-cli")
	if private {
		req.Header.Set("X-Optiqor-Private", "1")
	}

	client := &http.Client{Timeout: uploadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return UploadResult{Hash: hash, URL: url, Error: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UploadResult{Hash: hash, URL: url, Error: fmt.Sprintf("upload rejected: HTTP %d", resp.StatusCode)}
	}
	return UploadResult{Hash: hash, URL: url, Posted: true}
}

// IsHash reports whether s looks like a hash this package would emit.
func IsHash(s string) bool {
	if len(s) != HashLen {
		return false
	}
	s = strings.ToLower(s)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
