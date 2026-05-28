package ratelimit

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/optiqor/optiqor/internal/platform/httperr"
	"github.com/optiqor/optiqor/internal/platform/logging"
)

// Limiter implementations must be concurrent-safe. An over-limit
// decision is (false, retry, nil); errors are reserved for transport
// failures so the middleware can fail-open against a flapping backend.
type Limiter interface {
	Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration, err error)
}

// Quotaer exposes the remaining-quota state for a key. Optional —
// implementations not satisfying it lose the X-RateLimit-Remaining
// header but every other rate-limit signal still works. Memory
// implements it.
type Quotaer interface {
	Quota(ctx context.Context, key string) (limit, remaining int, resetAt time.Time)
}

type KeyFn func(r *http.Request) string

// ClientIPKey returns the left-most non-empty X-Forwarded-For entry, or
// the host of RemoteAddr. An empty result tells Middleware to bypass so
// a misconfigured proxy can't block all traffic at once.
func ClientIPKey(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, raw := range strings.Split(xff, ",") {
			ip := strings.TrimSpace(raw)
			if ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type Options struct {
	Limiter Limiter
	// KeyFn defaults to ClientIPKey.
	KeyFn KeyFn
	// FailOpen passes the request through on limiter error rather than
	// 503-ing the public surface. Recommended for the sandbox.
	FailOpen bool
	// HeaderName defaults to RFC 9110 §10.2.3 "Retry-After".
	HeaderName string
}

// Middleware writes the conventional X-RateLimit-* trio + Retry-After
// on every response so clients can pace themselves, and emits a
// structured JSON 429 envelope on rejection.
func Middleware(opts Options) func(http.Handler) http.Handler {
	if opts.Limiter == nil {
		panic("ratelimit: Limiter is required")
	}
	if opts.KeyFn == nil {
		opts.KeyFn = ClientIPKey
	}
	if opts.HeaderName == "" {
		opts.HeaderName = "Retry-After"
	}
	quotaer, _ := opts.Limiter.(Quotaer)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := opts.KeyFn(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			allowed, retry, err := opts.Limiter.Allow(r.Context(), key)
			if err != nil {
				if opts.FailOpen {
					next.ServeHTTP(w, r)
					return
				}
				httperr.ServiceUnavailable(w, r, "rate limiter unavailable")
				return
			}
			if quotaer != nil {
				writeQuotaHeaders(w, quotaer, r.Context(), key)
			}
			if !allowed {
				secs := int64(retry.Seconds())
				if secs < 1 {
					secs = 1
				}
				w.Header().Set(opts.HeaderName, strconv.FormatInt(secs, 10))
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				if reqID := logging.RequestIDFromContext(r.Context()); reqID != "" {
					w.Header().Set("X-Request-ID", reqID)
				}
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(httperr.Envelope{Error: httperr.Error{
					Code:       httperr.CodeRateLimited,
					Message:    "rate limit exceeded; retry after " + strconv.FormatInt(secs, 10) + " seconds",
					StatusCode: http.StatusTooManyRequests,
					RequestID:  logging.RequestIDFromContext(r.Context()),
					Details: map[string]any{
						"retry_after_seconds": secs,
					},
				}})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeQuotaHeaders(w http.ResponseWriter, q Quotaer, ctx context.Context, key string) {
	limit, remaining, resetAt := q.Quota(ctx, key)
	if limit <= 0 {
		return
	}
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
	if remaining < 0 {
		remaining = 0
	}
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
	if !resetAt.IsZero() {
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))
	}
}
