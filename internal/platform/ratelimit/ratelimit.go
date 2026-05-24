package ratelimit

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Limiter implementations must be concurrent-safe. An over-limit
// decision is (false, retry, nil); errors are reserved for transport
// failures so the middleware can fail-open against a flapping backend.
type Limiter interface {
	Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration, err error)
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

// Middleware writes Retry-After in seconds (minimum 1) on a 429.
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
				http.Error(w, "rate limiter unavailable", http.StatusServiceUnavailable)
				return
			}
			if !allowed {
				secs := int64(retry.Seconds())
				if secs < 1 {
					secs = 1
				}
				w.Header().Set(opts.HeaderName, strconv.FormatInt(secs, 10))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
