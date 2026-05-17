// Package logging provides the slog handler that injects tenant_id,
// request_id, and workflow_id from context into every log line.
//
// The handler is a thin wrapper around the stdlib slog.JSONHandler. It
// reads context-attached IDs and adds them as structured attributes on
// every record automatically — domain code never needs to remember to
// log them by hand.
package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Field names emitted on every record. Stable so log indexing in Loki
// can be aliased to these keys.
const (
	AttrTenantID   = "tenant_id"
	AttrWorkspace  = "workspace_id"
	AttrCluster    = "cluster_id"
	AttrNamespace  = "namespace"
	AttrRequestID  = "request_id"
	AttrWorkflowID = "workflow_id"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	workflowIDKey
)

// WithRequestID attaches an HTTP request ID. Middleware sets this once
// per request; downstream handlers, DB calls, LLM calls all inherit it.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey, id)
}

// WithWorkflowID attaches a Temporal workflow ID. The worker sets this
// once per workflow invocation.
func WithWorkflowID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, workflowIDKey, id)
}

// New returns a slog.Logger that emits JSON to w and auto-injects
// tenant + request + workflow IDs from context. lvl is "debug" |
// "info" | "warn" | "error" (case-insensitive).
func New(w io.Writer, lvl string) *slog.Logger {
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parseLevel(lvl)})
	return slog.New(&contextHandler{Handler: base})
}

// contextHandler enriches every record with attributes pulled from the
// context. It does not allocate when the context carries no IDs.
type contextHandler struct {
	slog.Handler
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if t, err := tenancy.FromContext(ctx); err == nil {
		r.AddAttrs(slog.String(AttrTenantID, t.TenantID))
		if t.WorkspaceID != "" {
			r.AddAttrs(slog.String(AttrWorkspace, t.WorkspaceID))
		}
		if t.ClusterID != "" {
			r.AddAttrs(slog.String(AttrCluster, t.ClusterID))
		}
		if t.Namespace != "" {
			r.AddAttrs(slog.String(AttrNamespace, t.Namespace))
		}
	}
	if v, ok := ctx.Value(requestIDKey).(string); ok && v != "" {
		r.AddAttrs(slog.String(AttrRequestID, v))
	}
	if v, ok := ctx.Value(workflowIDKey).(string); ok && v != "" {
		r.AddAttrs(slog.String(AttrWorkflowID, v))
	}
	return h.Handler.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
