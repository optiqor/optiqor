package config

import (
	"strings"
	"testing"
	"time"
)

func TestEnv_Valid(t *testing.T) {
	cases := map[Env]bool{
		EnvDev:     true,
		EnvStaging: true,
		EnvProd:    true,
		Env(""):    false,
		Env("qa"):  false,
	}
	for e, want := range cases {
		if got := e.Valid(); got != want {
			t.Errorf("Env(%q).Valid() = %v, want %v", e, got, want)
		}
	}
}

func TestValidate_DevDefaults(t *testing.T) {
	c := Config{
		Env:           EnvDev,
		LogLevel:      "debug",
		ShutdownGrace: 5 * time.Second,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("dev defaults should validate, got: %v", err)
	}
}

func TestValidate_ProdRequiresSecrets(t *testing.T) {
	c := Config{
		Env:           EnvProd,
		LogLevel:      "info",
		ShutdownGrace: 10 * time.Second,
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("prod without secrets must fail")
	}
	for _, want := range []string{"POSTGRES_DSN", "ANTHROPIC_API_KEY", "GITHUB_APP_ID"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in error: %v", want, err)
		}
	}
}

func TestValidate_BadLogLevel(t *testing.T) {
	c := Config{Env: EnvDev, LogLevel: "spew", ShutdownGrace: time.Second}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("expected log-level error, got: %v", err)
	}
}

func TestValidate_BadEnv(t *testing.T) {
	c := Config{Env: Env("qa"), LogLevel: "info", ShutdownGrace: time.Second}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "OPTIQOR_ENV") {
		t.Fatalf("expected env error, got: %v", err)
	}
}

func TestValidate_NonPositiveShutdownGrace(t *testing.T) {
	c := Config{Env: EnvDev, LogLevel: "info", ShutdownGrace: 0}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "SHUTDOWN_GRACE") {
		t.Fatalf("expected shutdown-grace error, got: %v", err)
	}
}

func TestLoad_Dev(t *testing.T) {
	t.Setenv("OPTIQOR_ENV", "dev")
	t.Setenv("OPTIQOR_LOG_LEVEL", "debug")
	t.Setenv("OPTIQOR_HTTP_ADDR", ":18080")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Env != EnvDev || c.LogLevel != "debug" || c.HTTPAddr != ":18080" {
		t.Fatalf("Load did not honor env: %+v", c)
	}
	if c.ShutdownGrace != 10*time.Second {
		t.Errorf("default ShutdownGrace = %v, want 10s", c.ShutdownGrace)
	}
}

func TestLoad_DurationOverride(t *testing.T) {
	t.Setenv("OPTIQOR_ENV", "dev")
	t.Setenv("OPTIQOR_SHUTDOWN_GRACE", "30s")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ShutdownGrace != 30*time.Second {
		t.Errorf("ShutdownGrace = %v, want 30s", c.ShutdownGrace)
	}
}

func TestLoad_DurationFallbackOnGarbage(t *testing.T) {
	t.Setenv("OPTIQOR_ENV", "dev")
	t.Setenv("OPTIQOR_SHUTDOWN_GRACE", "not-a-duration")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ShutdownGrace != 10*time.Second {
		t.Errorf("garbage duration should fall back to default; got %v", c.ShutdownGrace)
	}
}
