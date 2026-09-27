package egress

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/agent/ident"
	"github.com/optiqor/optiqor/internal/ingestion"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// testClient bypasses the real mTLS path so the test exercises the
// JSON + token wiring without minting certs. PostSnapshot is the only
// public surface that depends on the http client; we replace it.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	iss, err := ident.NewIssuer([]byte(testSecret), ident.MaxTTL)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	return &Client{
		cfg: Config{
			BackendURL:   srv.URL,
			TenantID:     "11111111-1111-1111-1111-111111111111",
			ClusterID:    "c1",
			AgentVersion: "test",
			IngestSecret: []byte(testSecret),
			Timeout:      5_000_000_000,
		},
		httpClient: srv.Client(),
		issuer:     iss,
	}
}

func TestPostSnapshot_SendsTokenAndReceivesAck(t *testing.T) {
	var captured struct {
		token       string
		contentType string
		body        ingestion.AgentSnapshot
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.token = r.Header.Get(ingestion.TokenHeader)
		captured.contentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&captured.body)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(ingestion.AgentSnapshotResponse{
			Accepted:     true,
			BatchID:      "b1",
			WorkloadsObs: 2,
		})
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)
	got, err := c.PostSnapshot(context.Background(), ingestion.AgentSnapshot{
		BatchID:   "b1",
		ClusterID: "c1",
		Workloads: []ingestion.WorkloadSnap{
			{Namespace: "prod", Kind: "Deployment", Name: "api"},
			{Namespace: "prod", Kind: "Deployment", Name: "worker"},
		},
	})
	if err != nil {
		t.Fatalf("PostSnapshot: %v", err)
	}
	if !got.Accepted || got.WorkloadsObs != 2 {
		t.Errorf("resp = %+v", got)
	}
	if captured.token == "" || !strings.Contains(captured.token, ".") {
		t.Errorf("token header missing: %q", captured.token)
	}
	if captured.contentType != "application/json" {
		t.Errorf("content-type = %q", captured.contentType)
	}
	if captured.body.BatchID != "b1" || len(captured.body.Workloads) != 2 {
		t.Errorf("server saw unexpected snapshot: %+v", captured.body)
	}
}

func TestPostSnapshot_BackendError_PropagatesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"BAD"}}`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)
	_, err := c.PostSnapshot(context.Background(), ingestion.AgentSnapshot{BatchID: "b1", ClusterID: "c1"})
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("want 400 error, got %v", err)
	}
}
