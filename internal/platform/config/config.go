// Package config loads and validates runtime configuration for all Optiqor
// binaries (api, worker, agent).
//
// Real production secrets come from AWS Secrets Manager and are injected
// into the environment at boot. This package only knows how to read env
// vars and validate them — it has no AWS dependency.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Env identifies the runtime environment. Used for environment-aware
// safety profiles and observability tagging.
type Env string

const (
	EnvDev     Env = "dev"
	EnvStaging Env = "staging"
	EnvProd    Env = "prod"
)

// Valid reports whether e is a recognised environment.
func (e Env) Valid() bool {
	switch e {
	case EnvDev, EnvStaging, EnvProd:
		return true
	}
	return false
}

// Config is the typed configuration for a Optiqor binary. Fields are
// populated from environment variables prefixed with `OPTIQOR_`.
type Config struct {
	Env         Env
	LogLevel    string // debug | info | warn | error
	HTTPAddr    string // e.g. ":8080"
	MetricsAddr string // e.g. ":9090"

	PostgresDSN  string
	RedisAddr    string
	TemporalHost string

	AnthropicAPIKey string

	GitHubAppID            string
	GitHubAppClientID      string
	GitHubAppClientSecret  string
	GitHubAppPrivateKeyPEM string
	GitHubAppWebhookSecret string

	AWSRegion string
	SentryDSN string
	OTELHTTP  string

	ShutdownGrace time.Duration
}

// Load reads OPTIQOR_* env vars and returns a validated Config.
//
// Returns an error if any required field is missing or invalid. Callers
// (cmd/*/main.go) should treat this as a fatal startup error.
func Load() (Config, error) {
	c := Config{
		Env:                    Env(envOr("OPTIQOR_ENV", "dev")),
		LogLevel:               envOr("OPTIQOR_LOG_LEVEL", "info"),
		HTTPAddr:               envOr("OPTIQOR_HTTP_ADDR", ":8080"),
		MetricsAddr:            envOr("OPTIQOR_METRICS_ADDR", ":9090"),
		PostgresDSN:            os.Getenv("OPTIQOR_POSTGRES_DSN"),
		RedisAddr:              envOr("OPTIQOR_REDIS_ADDR", "localhost:6379"),
		TemporalHost:           envOr("OPTIQOR_TEMPORAL_HOSTPORT", "localhost:7233"),
		AnthropicAPIKey:        os.Getenv("OPTIQOR_ANTHROPIC_API_KEY"),
		GitHubAppID:            os.Getenv("OPTIQOR_GITHUB_APP_ID"),
		GitHubAppClientID:      os.Getenv("OPTIQOR_GITHUB_APP_CLIENT_ID"),
		GitHubAppClientSecret:  os.Getenv("OPTIQOR_GITHUB_APP_CLIENT_SECRET"),
		GitHubAppPrivateKeyPEM: os.Getenv("OPTIQOR_GITHUB_APP_PRIVATE_KEY_PEM"),
		GitHubAppWebhookSecret: os.Getenv("OPTIQOR_GITHUB_APP_WEBHOOK_SECRET"),
		AWSRegion:              envOr("AWS_REGION", "us-east-1"),
		SentryDSN:              os.Getenv("OPTIQOR_SENTRY_DSN"),
		OTELHTTP:               envOr("OPTIQOR_OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		ShutdownGrace:          envDuration("OPTIQOR_SHUTDOWN_GRACE", 10*time.Second),
	}

	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Validate runs all field-level checks. Exposed so tests can build a
// Config in-memory and validate without going through the env.
func (c Config) Validate() error {
	var errs []string

	if !c.Env.Valid() {
		errs = append(errs, fmt.Sprintf("invalid OPTIQOR_ENV %q (want dev|staging|prod)", c.Env))
	}

	if !validLogLevel(c.LogLevel) {
		errs = append(errs, fmt.Sprintf("invalid OPTIQOR_LOG_LEVEL %q (want debug|info|warn|error)", c.LogLevel))
	}

	if c.Env == EnvProd {
		// Prod requires the full secret set. Dev/staging may omit them.
		if c.PostgresDSN == "" {
			errs = append(errs, "OPTIQOR_POSTGRES_DSN is required in prod")
		}
		if c.AnthropicAPIKey == "" {
			errs = append(errs, "OPTIQOR_ANTHROPIC_API_KEY is required in prod")
		}
		if c.GitHubAppID == "" {
			errs = append(errs, "OPTIQOR_GITHUB_APP_ID is required in prod")
		}
	}

	if c.ShutdownGrace <= 0 {
		errs = append(errs, "OPTIQOR_SHUTDOWN_GRACE must be positive")
	}

	if len(errs) > 0 {
		return errors.New("config: " + strings.Join(errs, "; "))
	}
	return nil
}

func validLogLevel(s string) bool {
	switch strings.ToLower(s) {
	case "debug", "info", "warn", "error":
		return true
	}
	return false
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

// envInt is reserved for future numeric config; kept here so the
// config package is self-contained as more fields land.
func envInt(key string, fallback int) int { //nolint:unused // reserved for future numeric config fields
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
