// Package egress is the agent's outbound HTTPS client. mTLS material
// arrives via a Kubernetes Secret mount at /var/run/optiqor/mtls; the
// short-lived JWT is signed locally per batch by internal/agent/ident.
// One Client per agent process; reuse the transport so the connection
// pool primes on first call and stays warm thereafter.
package egress

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/optiqor/optiqor/internal/agent/ident"
	"github.com/optiqor/optiqor/internal/ingestion"
)

// Config wires the egress client. Defaults to /var/run/optiqor/mtls
// for the cert + key + ca, which matches deploy/helm/optiqor-agent.
type Config struct {
	BackendURL   string
	TenantID     string
	ClusterID    string
	AgentVersion string
	CertPath     string
	KeyPath      string
	CABundlePath string
	// IngestSecret is the HS256 shared secret. Mounted from the same
	// Kubernetes Secret as the mTLS material so the agent reads both
	// in one place. The api side verifies tokens with the matching
	// secret loaded from AWS Secrets Manager.
	IngestSecret []byte
	// Timeout caps the wall time per POST; default 30s.
	Timeout time.Duration
}

// Default mount paths inside the agent pod. Override via the values
// file if a customer's cert tooling places them elsewhere.
const (
	DefaultMTLSDir = "/var/run/optiqor/mtls"
	DefaultCert    = "tls.crt"
	DefaultKey     = "tls.key"
	DefaultCA      = "ca.crt"
)

// Client posts AgentSnapshot payloads to the SaaS api. Concurrent-safe.
type Client struct {
	cfg        Config
	httpClient *http.Client
	issuer     *ident.Issuer
}

func New(cfg Config) (*Client, error) {
	if cfg.BackendURL == "" {
		return nil, errors.New("egress: BackendURL is required")
	}
	if cfg.TenantID == "" {
		return nil, errors.New("egress: TenantID is required")
	}
	if cfg.CertPath == "" {
		cfg.CertPath = filepath.Join(DefaultMTLSDir, DefaultCert)
	}
	if cfg.KeyPath == "" {
		cfg.KeyPath = filepath.Join(DefaultMTLSDir, DefaultKey)
	}
	if cfg.CABundlePath == "" {
		cfg.CABundlePath = filepath.Join(DefaultMTLSDir, DefaultCA)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	tlsCfg, err := buildTLS(cfg)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		TLSClientConfig:    tlsCfg,
		MaxIdleConns:       4,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: false,
	}
	iss, err := ident.NewIssuer(cfg.IngestSecret, ident.MaxTTL)
	if err != nil {
		return nil, fmt.Errorf("egress: ident: %w", err)
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Transport: transport, Timeout: cfg.Timeout},
		issuer:     iss,
	}, nil
}

func buildTLS(cfg Config) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("egress: load client cert: %w", err)
	}
	pool := x509.NewCertPool()
	if cfg.CABundlePath != "" {
		ca, err := os.ReadFile(cfg.CABundlePath)
		if err != nil {
			return nil, fmt.Errorf("egress: read CA bundle: %w", err)
		}
		if !pool.AppendCertsFromPEM(ca) {
			return nil, errors.New("egress: CA bundle PEM did not contain any certs")
		}
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// PostSnapshot sends one AgentSnapshot batch. Signature: 15-min HS256
// JWT (audience "agent-snapshot") + mTLS client cert. Returns the
// parsed AgentSnapshotResponse so callers can log obs counts.
func (c *Client) PostSnapshot(ctx context.Context, snap ingestion.AgentSnapshot) (ingestion.AgentSnapshotResponse, error) {
	body, err := json.Marshal(snap)
	if err != nil {
		return ingestion.AgentSnapshotResponse{}, fmt.Errorf("egress: marshal: %w", err)
	}
	tok, err := c.issuer.Issue(c.cfg.TenantID, c.cfg.ClusterID, snap.BatchID, ingestion.AgentSnapshotAudience)
	if err != nil {
		return ingestion.AgentSnapshotResponse{}, fmt.Errorf("egress: issue token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BackendURL+"/v1/agent/snapshot", bytes.NewReader(body))
	if err != nil {
		return ingestion.AgentSnapshotResponse{}, fmt.Errorf("egress: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(ingestion.TokenHeader, tok)
	req.Header.Set("User-Agent", "optiqor-agent/"+c.cfg.AgentVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ingestion.AgentSnapshotResponse{}, fmt.Errorf("egress: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ingestion.AgentSnapshotResponse{}, fmt.Errorf("egress: read: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return ingestion.AgentSnapshotResponse{}, fmt.Errorf("egress: backend %d: %s", resp.StatusCode, string(respBody))
	}
	var out ingestion.AgentSnapshotResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return ingestion.AgentSnapshotResponse{}, fmt.Errorf("egress: unmarshal: %w", err)
	}
	return out, nil
}
