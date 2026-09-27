package ingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/optiqor/optiqor/internal/agent/ident"
	"github.com/optiqor/optiqor/internal/tenancy"
)

const sinkSecret = "0123456789abcdef0123456789abcdef"

type captureSink struct {
	mu   sync.Mutex
	snap AgentSnapshot
	tc   tenancy.Context
	err  error
}

func (c *captureSink) Persist(t tenancy.Context, s AgentSnapshot) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snap = s
	c.tc = t
	return c.err
}

func issueToken(t *testing.T, tenantID, clusterID, batchID string) string {
	t.Helper()
	iss, err := ident.NewIssuer([]byte(sinkSecret), 5*time.Minute)
	if err != nil {
		t.Fatalf("NewIssuer: %v", err)
	}
	tok, err := iss.Issue(tenantID, clusterID, batchID, AgentSnapshotAudience)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	return tok
}

func TestAgentSnapshot_HappyPath_PersistsAndAcks(t *testing.T) {
	ver, _ := ident.NewVerifier([]byte(sinkSecret))
	sink := &captureSink{}
	h := &AgentSnapshotHandler{Verifier: ver, Sink: sink}

	tenantID := "11111111-1111-1111-1111-111111111111"
	clusterID := "22222222-2222-2222-2222-222222222222"
	batchID := "b-001"
	tokStr := issueToken(t, tenantID, clusterID, batchID)

	body, _ := json.Marshal(AgentSnapshot{
		BatchID:    batchID,
		ClusterID:  clusterID,
		CapturedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		Workloads:  []WorkloadSnap{{Namespace: "prod", Kind: "Deployment", Name: "api"}},
		Events:     []EventSnap{{Namespace: "prod", PodName: "api-x", Reason: "OOMKilled", Count: 1}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/agent/snapshot", bytes.NewReader(body))
	req.Header.Set(TokenHeader, tokStr)
	req = req.WithContext(tenancy.WithContext(context.Background(), tenancy.Context{TenantID: tenantID}))
	rec := httptest.NewRecorder()
	h.Snapshot(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var resp AgentSnapshotResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if !resp.Accepted || resp.BatchID != batchID || resp.WorkloadsObs != 1 || resp.EventsObs != 1 {
		t.Errorf("resp = %+v", resp)
	}
	if sink.snap.BatchID != batchID || sink.tc.TenantID != tenantID || sink.tc.ClusterID != clusterID {
		t.Errorf("sink received unexpected: %+v / %+v", sink.snap, sink.tc)
	}
}

func TestAgentSnapshot_MissingToken_401(t *testing.T) {
	ver, _ := ident.NewVerifier([]byte(sinkSecret))
	h := &AgentSnapshotHandler{Verifier: ver, Sink: &captureSink{}}
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/snapshot", strings.NewReader("{}"))
	req = req.WithContext(tenancy.WithContext(context.Background(), tenancy.Context{TenantID: "t1"}))
	rec := httptest.NewRecorder()
	h.Snapshot(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestAgentSnapshot_TokenTenantMismatch_401(t *testing.T) {
	ver, _ := ident.NewVerifier([]byte(sinkSecret))
	h := &AgentSnapshotHandler{Verifier: ver, Sink: &captureSink{}}
	tokStr := issueToken(t, "other-tenant-uuid", "c1", "b1")

	body, _ := json.Marshal(AgentSnapshot{BatchID: "b1", ClusterID: "c1"})
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/snapshot", bytes.NewReader(body))
	req.Header.Set(TokenHeader, tokStr)
	req = req.WithContext(tenancy.WithContext(context.Background(), tenancy.Context{TenantID: "cert-tenant-uuid"}))
	rec := httptest.NewRecorder()
	h.Snapshot(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "does not match the mTLS tenant") && !strings.Contains(rec.Body.String(), "does not match") {
		t.Errorf("body lacks mismatch msg: %s", rec.Body.String())
	}
}

func TestAgentSnapshot_ClusterMismatch_400(t *testing.T) {
	ver, _ := ident.NewVerifier([]byte(sinkSecret))
	h := &AgentSnapshotHandler{Verifier: ver, Sink: &captureSink{}}
	tenantID := "11111111-1111-1111-1111-111111111111"
	tokStr := issueToken(t, tenantID, "cluster-A", "b1")

	body, _ := json.Marshal(AgentSnapshot{BatchID: "b1", ClusterID: "cluster-B"})
	req := httptest.NewRequest(http.MethodPost, "/v1/agent/snapshot", bytes.NewReader(body))
	req.Header.Set(TokenHeader, tokStr)
	req = req.WithContext(tenancy.WithContext(context.Background(), tenancy.Context{TenantID: tenantID}))
	rec := httptest.NewRecorder()
	h.Snapshot(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestAgentSnapshot_SinkError_500(t *testing.T) {
	ver, _ := ident.NewVerifier([]byte(sinkSecret))
	sink := &captureSink{err: errors.New("db down")}
	h := &AgentSnapshotHandler{Verifier: ver, Sink: sink}
	tenantID := "11111111-1111-1111-1111-111111111111"
	tokStr := issueToken(t, tenantID, "c1", "b1")
	body, _ := json.Marshal(AgentSnapshot{BatchID: "b1", ClusterID: "c1"})

	req := httptest.NewRequest(http.MethodPost, "/v1/agent/snapshot", bytes.NewReader(body))
	req.Header.Set(TokenHeader, tokStr)
	req = req.WithContext(tenancy.WithContext(context.Background(), tenancy.Context{TenantID: tenantID}))
	rec := httptest.NewRecorder()
	h.Snapshot(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d", rec.Code)
	}
}
