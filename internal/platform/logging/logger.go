package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/optiqor/optiqor/internal/tenancy"
)

// Attribute names. Stable so Loki indexers can alias to them.
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

// WithRequestID is set once by middleware; downstream handlers, DB
// calls, and LLM calls inherit it through context.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey, id)
}

// WithWorkflowID is set once per workflow invocation by the worker.
func WithWorkflowID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, workflowIDKey, id)
}

// New returns a JSON slog.Logger with the context auto-injection
// handler. lvl is debug|info|warn|error (case-insensitive).
func New(w io.Writer, lvl string) *slog.Logger {
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parseLevel(lvl)})
	return slog.New(&contextHandler{Handler: base})
}

// contextHandler is allocation-free when the context carries no IDs.
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
