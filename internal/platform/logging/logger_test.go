package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lowplane/backend/internal/tenancy"
)

// decode parses a single JSON log line into a map.
func decode(t *testing.T, line []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("decode log line: %v\nline: %s", err, line)
	}
	return m
}

func TestLogger_InjectsTenant(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "info")

	ctx := tenancy.WithContext(context.Background(), tenancy.Context{
		TenantID: "t1", WorkspaceID: "w1", ClusterID: "c1", Namespace: "ns1",
	})
	log.InfoContext(ctx, "hello")

	m := decode(t, buf.Bytes())
	if m[AttrTenantID] != "t1" {
		t.Errorf("tenant_id = %v, want t1", m[AttrTenantID])
	}
	if m[AttrWorkspace] != "w1" {
		t.Errorf("workspace_id = %v", m[AttrWorkspace])
	}
	if m[AttrCluster] != "c1" {
		t.Errorf("cluster_id = %v", m[AttrCluster])
	}
	if m[AttrNamespace] != "ns1" {
		t.Errorf("namespace = %v", m[AttrNamespace])
	}
	if m["msg"] != "hello" {
		t.Errorf("msg = %v", m["msg"])
	}
}

func TestLogger_OmitsEmptyAttrs(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "info")
	ctx := tenancy.WithContext(context.Background(), tenancy.Context{TenantID: "t1"})
	log.InfoContext(ctx, "hello")

	m := decode(t, buf.Bytes())
	if _, ok := m[AttrWorkspace]; ok {
		t.Error("workspace_id should be omitted when empty")
	}
	if _, ok := m[AttrCluster]; ok {
		t.Error("cluster_id should be omitted when empty")
	}
}

func TestLogger_RequestAndWorkflowIDs(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "info")

	ctx := WithRequestID(context.Background(), "req-1")
	ctx = WithWorkflowID(ctx, "wf-2")
	log.InfoContext(ctx, "hello")

	m := decode(t, buf.Bytes())
	if m[AttrRequestID] != "req-1" {
		t.Errorf("request_id = %v", m[AttrRequestID])
	}
	if m[AttrWorkflowID] != "wf-2" {
		t.Errorf("workflow_id = %v", m[AttrWorkflowID])
	}
}

func TestLogger_RespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "warn")

	log.Info("info-line")
	if buf.Len() != 0 {
		t.Errorf("info line should be filtered at warn level: %s", buf.String())
	}

	log.Warn("warn-line")
	if !strings.Contains(buf.String(), "warn-line") {
		t.Errorf("warn line should be emitted: %s", buf.String())
	}
}

func TestLogger_NoTenantNoCrash(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "info")
	log.InfoContext(context.Background(), "no-tenant")
	m := decode(t, buf.Bytes())
	if _, ok := m[AttrTenantID]; ok {
		t.Errorf("tenant_id should be absent when no tenant in context: %v", m)
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]string{
		"DEBUG":   "DEBUG",
		"info":    "INFO",
		"WARN":    "WARN",
		"error":   "ERROR",
		"garbage": "INFO",
	}
	for in, wantStr := range cases {
		if got := parseLevel(in); got.String() != wantStr {
			t.Errorf("parseLevel(%q) = %v, want %s", in, got, wantStr)
		}
	}
}

func TestLogger_WithAttrs_PreservesContextAttrs(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, "info").With("svc", "api")

	ctx := tenancy.WithContext(context.Background(), tenancy.Context{TenantID: "t1"})
	log.InfoContext(ctx, "hello")

	m := decode(t, buf.Bytes())
	if m["svc"] != "api" {
		t.Errorf("svc attr lost: %v", m)
	}
	if m[AttrTenantID] != "t1" {
		t.Errorf("tenant_id should still inject: %v", m)
	}
}
