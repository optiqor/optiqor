// Package config loads OPTIQOR_* env vars and validates them at boot.
// Real prod secrets come from AWS Secrets Manager and are injected into
// the environment before Load runs; this package has no AWS dependency.
package config
