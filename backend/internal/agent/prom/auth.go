package prom

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// AuthMode selects how the Prometheus client authenticates. Production
// customer clusters rarely run unauthenticated Prometheus; the install
// runbook documents each mode and how to scope the bearer token /
// ServiceAccount minimally (read on /api/v1/query + /api/v1/query_range,
// nothing else).
type AuthMode string

const (
	AuthNone    AuthMode = "none"
	AuthBearer  AuthMode = "bearer"
	AuthBasic   AuthMode = "basic"
	AuthSAToken AuthMode = "sa_token"
	AuthMTLS    AuthMode = "mtls"
)

// AuthConfig captures every supported credential shape in one struct so
// the agent's flag parsing stays a single env-to-config translation.
// Per-mode required fields are checked by NewAuthenticator.
type AuthConfig struct {
	Mode AuthMode

	BearerToken string
	BearerFile  string

	Username string
	Password string

	SATokenFile string

	ClientCertFile string
	ClientKeyFile  string
	CABundleFile   string

	// TokenRefresh sets how often the SA-token mode re-reads the file
	// on disk. The kubelet rotates projected ServiceAccount tokens
	// roughly hourly; we refresh every minute by default so a rotation
	// can't make a long-lived loop start 401-ing.
	TokenRefresh time.Duration
}

// Authenticator mutates an outgoing request so Prometheus accepts it.
// Implementations are safe for concurrent use; the file-watching ones
// guard their cached state with a mutex.
type Authenticator interface {
	Apply(*http.Request) error
}

// NewAuthenticator validates cfg and returns the matching implementation.
// Empty Mode falls through to AuthNone — that's the dev path against
// `localhost:9090` Prometheus with no auth.
func NewAuthenticator(cfg AuthConfig) (Authenticator, error) {
	mode := cfg.Mode
	if mode == "" {
		mode = AuthNone
	}
	switch mode {
	case AuthNone:
		return noopAuth{}, nil
	case AuthBearer:
		if cfg.BearerToken == "" && cfg.BearerFile == "" {
			return nil, errors.New("prom/auth: bearer requires BearerToken or BearerFile")
		}
		if cfg.BearerFile != "" {
			refresh := cfg.TokenRefresh
			if refresh <= 0 {
				refresh = time.Minute
			}
			return newFileTokenAuth(cfg.BearerFile, "Bearer", refresh), nil
		}
		return &headerAuth{header: "Authorization", value: "Bearer " + cfg.BearerToken}, nil
	case AuthBasic:
		if cfg.Username == "" {
			return nil, errors.New("prom/auth: basic requires Username")
		}
		return &basicAuth{username: cfg.Username, password: cfg.Password}, nil
	case AuthSAToken:
		file := cfg.SATokenFile
		if file == "" {
			file = "/var/run/secrets/kubernetes.io/serviceaccount/token"
		}
		refresh := cfg.TokenRefresh
		if refresh <= 0 {
			refresh = time.Minute
		}
		return newFileTokenAuth(file, "Bearer", refresh), nil
	case AuthMTLS:
		if cfg.ClientCertFile == "" || cfg.ClientKeyFile == "" {
			return nil, errors.New("prom/auth: mtls requires ClientCertFile + ClientKeyFile")
		}
		// mTLS lives on the TLS config of the http.Transport, not on
		// per-request headers. Apply is a no-op; the HTTPClient wires
		// the cert into its transport at construction.
		return mtlsAuth{}, nil
	default:
		return nil, fmt.Errorf("prom/auth: unknown mode %q", mode)
	}
}

// IsMTLS lets HTTPClient detect that it must build its own transport.
func IsMTLS(a Authenticator) bool {
	_, ok := a.(mtlsAuth)
	return ok
}

// BuildTLSConfig assembles the *tls.Config for mTLS mode. CA bundle is
// optional; empty means trust the host roots, which is what private CAs
// configured via the OS truststore want.
func BuildTLSConfig(cfg AuthConfig) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
	if err != nil {
		return nil, fmt.Errorf("prom/auth: load client cert: %w", err)
	}
	out := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if cfg.CABundleFile != "" {
		pem, err := os.ReadFile(cfg.CABundleFile)
		if err != nil {
			return nil, fmt.Errorf("prom/auth: read CA bundle: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("prom/auth: CA bundle has no PEM blocks")
		}
		out.RootCAs = pool
	}
	return out, nil
}

type noopAuth struct{}

func (noopAuth) Apply(*http.Request) error { return nil }

type mtlsAuth struct{}

func (mtlsAuth) Apply(*http.Request) error { return nil }

type headerAuth struct {
	header string
	value  string
}

func (h *headerAuth) Apply(r *http.Request) error {
	r.Header.Set(h.header, h.value)
	return nil
}

type basicAuth struct {
	username string
	password string
}

func (b *basicAuth) Apply(r *http.Request) error {
	r.SetBasicAuth(b.username, b.password)
	return nil
}

// fileTokenAuth reads a bearer token from disk and refreshes it on a
// timer. The Kubernetes projected-token volume rewrites the file in
// place every ~hour; rereading every minute is enough headroom to never
// 401 mid-flight, cheap enough that nobody notices.
type fileTokenAuth struct {
	path    string
	scheme  string
	refresh time.Duration

	mu        sync.RWMutex
	cached    string
	cachedAt  time.Time
	clock     func() time.Time
	readFile  func(string) ([]byte, error)
	cachedErr error
}

func newFileTokenAuth(path, scheme string, refresh time.Duration) *fileTokenAuth {
	return &fileTokenAuth{
		path:     path,
		scheme:   scheme,
		refresh:  refresh,
		clock:    time.Now,
		readFile: os.ReadFile,
	}
}

func (f *fileTokenAuth) Apply(r *http.Request) error {
	tok, err := f.token()
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", f.scheme+" "+tok)
	return nil
}

func (f *fileTokenAuth) token() (string, error) {
	now := f.clock()
	f.mu.RLock()
	tok := f.cached
	at := f.cachedAt
	cerr := f.cachedErr
	f.mu.RUnlock()
	if tok != "" && now.Sub(at) < f.refresh {
		return tok, nil
	}
	if cerr != nil && now.Sub(at) < f.refresh {
		return "", cerr
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	// Double-check under write lock; another goroutine may have refreshed
	// while we waited.
	if f.cached != "" && now.Sub(f.cachedAt) < f.refresh {
		return f.cached, nil
	}
	raw, err := f.readFile(f.path)
	if err != nil {
		f.cachedErr = err
		f.cachedAt = now
		return "", fmt.Errorf("prom/auth: read token %s: %w", f.path, err)
	}
	f.cached = strings.TrimSpace(string(raw))
	f.cachedAt = now
	f.cachedErr = nil
	if f.cached == "" {
		return "", fmt.Errorf("prom/auth: token file %s is empty", f.path)
	}
	return f.cached, nil
}
