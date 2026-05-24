package workflows

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/optiqor/optiqor/internal/tenancy"
	"github.com/optiqor/optiqor/internal/worker"
)

func TestEcho_Name(t *testing.T) {
	if NewEcho(nil).Name() != EchoName {
		t.Errorf("name drift: %q vs %q", NewEcho(nil).Name(), EchoName)
	}
}

func TestEcho_LogsTenantAndPayloadLength(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e := NewEcho(logger)

	ctx := context.Background()
	t1 := tenancy.Context{TenantID: "t1", WorkspaceID: "w1"}
	if err := e.Execute(ctx, t1, []byte("hello-payload")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("decode: %v\n%s", err, buf.String())
	}
	for _, tc := range []struct {
		name string
		key  string
		want any
	}{
		{"msg", "msg", "echo workflow"},
		{"tenant", "tenant_id", "t1"},
		{"workspace", "workspace_id", "w1"},
		{"payload length", "payload_len", float64(len("hello-payload"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if rec[tc.key] != tc.want {
				t.Errorf("%s = %v, want %v", tc.key, rec[tc.key], tc.want)
			}
		})
	}
}

func TestEcho_DispatcherRoundTrip(t *testing.T) {
	d := worker.NewInMemory()
	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := d.Register(NewEcho(silent)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := d.Submit(context.Background(), tenancy.Context{TenantID: "t1"}, worker.QueueDefault, EchoName, []byte("hi")); err != nil {
		t.Fatalf("Submit: %v", err)
	}
}
