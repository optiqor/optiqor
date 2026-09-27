package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
)

func decode(t *testing.T, line []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		t.Fatalf("decode log line: %v\nline: %s", err, line)
	}
	return m
}

func TestLogger_ContextAttrs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		emit    func(ctx context.Context, l *slog.Logger)
		ctx     func() context.Context
		wantHas map[string]any
		wantMis []string
	}{
		{
			name: "injects full tenancy",
			ctx: func() context.Context {
				return tenancy.WithContext(context.Background(), tenancy.Context{
					TenantID: "t1", WorkspaceID: "w1", ClusterID: "c1", Namespace: "ns1",
				})
			},
			emit: func(ctx context.Context, l *slog.Logger) { l.InfoContext(ctx, "hello") },
			wantHas: map[string]any{
				AttrTenantID:  "t1",
				AttrWorkspace: "w1",
				AttrCluster:   "c1",
				AttrNamespace: "ns1",
				"msg":         "hello",
			},
		},
		{
			name: "omits empty workspace and cluster",
			ctx: func() context.Context {
				return tenancy.WithContext(context.Background(), tenancy.Context{TenantID: "t1"})
			},
			emit:    func(ctx context.Context, l *slog.Logger) { l.InfoContext(ctx, "hello") },
			wantMis: []string{AttrWorkspace, AttrCluster},
		},
		{
			name: "request and workflow ids surfaced",
			ctx: func() context.Context {
				ctx := WithRequestID(context.Background(), "req-1")
				return WithWorkflowID(ctx, "wf-2")
			},
			emit: func(ctx context.Context, l *slog.Logger) { l.InfoContext(ctx, "hello") },
			wantHas: map[string]any{
				AttrRequestID:  "req-1",
				AttrWorkflowID: "wf-2",
			},
		},
		{
			name:    "no tenant in context omits tenant_id",
			ctx:     context.Background,
			emit:    func(ctx context.Context, l *slog.Logger) { l.InfoContext(ctx, "no-tenant") },
			wantMis: []string{AttrTenantID},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := New(&buf, "info")
			tc.emit(tc.ctx(), log)
			m := decode(t, buf.Bytes())
			for k, want := range tc.wantHas {
				if m[k] != want {
					t.Errorf("%s = %v, want %v", k, m[k], want)
				}
			}
			for _, k := range tc.wantMis {
				if _, ok := m[k]; ok {
					t.Errorf("%s should be absent: %v", k, m)
				}
			}
		})
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

func TestParseLevel(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"upper debug", "DEBUG", "DEBUG"},
		{"lower info", "info", "INFO"},
		{"upper warn", "WARN", "WARN"},
		{"lower error", "error", "ERROR"},
		{"unknown falls back to info", "garbage", "INFO"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseLevel(tc.in); got.String() != tc.want {
				t.Errorf("parseLevel(%q) = %v, want %s", tc.in, got, tc.want)
			}
		})
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
