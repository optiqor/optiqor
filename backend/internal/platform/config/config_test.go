package config

import (
	"strings"
	"testing"
	"time"
)

func TestEnv_Valid(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  Env
		want bool
	}{
		{"dev", EnvDev, true},
		{"staging", EnvStaging, true},
		{"prod", EnvProd, true},
		{"empty", Env(""), false},
		{"unknown", Env("qa"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.env.Valid(); got != tc.want {
				t.Errorf("Env(%q).Valid() = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cfg      Config
		wantErr  bool
		wantSubs []string
	}{
		{
			name: "dev defaults",
			cfg: Config{
				Env:           EnvDev,
				LogLevel:      "debug",
				ShutdownGrace: 5 * time.Second,
			},
		},
		{
			name: "prod requires secrets",
			cfg: Config{
				Env:           EnvProd,
				LogLevel:      "info",
				ShutdownGrace: 10 * time.Second,
			},
			wantErr:  true,
			wantSubs: []string{"POSTGRES_DSN", "ANTHROPIC_API_KEY", "GITHUB_APP_ID", "GITHUB_APP_WEBHOOK_SECRET"},
		},
		{
			name:     "bad log level",
			cfg:      Config{Env: EnvDev, LogLevel: "spew", ShutdownGrace: time.Second},
			wantErr:  true,
			wantSubs: []string{"LOG_LEVEL"},
		},
		{
			name:     "bad env",
			cfg:      Config{Env: Env("qa"), LogLevel: "info", ShutdownGrace: time.Second},
			wantErr:  true,
			wantSubs: []string{"OPTIQOR_ENV"},
		},
		{
			name:     "non-positive shutdown grace",
			cfg:      Config{Env: EnvDev, LogLevel: "info", ShutdownGrace: 0},
			wantErr:  true,
			wantSubs: []string{"SHUTDOWN_GRACE"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("validate: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			for _, want := range tc.wantSubs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("missing %q in error: %v", want, err)
				}
			}
		})
	}
}

func TestLoad(t *testing.T) {
	for _, tc := range []struct {
		name              string
		env               map[string]string
		wantEnv           Env
		wantLogLevel      string
		wantHTTPAddr      string
		wantShutdownGrace time.Duration
	}{
		{
			name: "dev env honored",
			env: map[string]string{
				"OPTIQOR_ENV":       "dev",
				"OPTIQOR_LOG_LEVEL": "debug",
				"OPTIQOR_HTTP_ADDR": ":18080",
			},
			wantEnv:           EnvDev,
			wantLogLevel:      "debug",
			wantHTTPAddr:      ":18080",
			wantShutdownGrace: 10 * time.Second,
		},
		{
			name: "duration override parses",
			env: map[string]string{
				"OPTIQOR_ENV":            "dev",
				"OPTIQOR_SHUTDOWN_GRACE": "30s",
			},
			wantShutdownGrace: 30 * time.Second,
		},
		{
			name: "garbage duration falls back to default",
			env: map[string]string{
				"OPTIQOR_ENV":            "dev",
				"OPTIQOR_SHUTDOWN_GRACE": "not-a-duration",
			},
			wantShutdownGrace: 10 * time.Second,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			c, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if tc.wantEnv != "" && c.Env != tc.wantEnv {
				t.Errorf("Env = %q, want %q", c.Env, tc.wantEnv)
			}
			if tc.wantLogLevel != "" && c.LogLevel != tc.wantLogLevel {
				t.Errorf("LogLevel = %q, want %q", c.LogLevel, tc.wantLogLevel)
			}
			if tc.wantHTTPAddr != "" && c.HTTPAddr != tc.wantHTTPAddr {
				t.Errorf("HTTPAddr = %q, want %q", c.HTTPAddr, tc.wantHTTPAddr)
			}
			if c.ShutdownGrace != tc.wantShutdownGrace {
				t.Errorf("ShutdownGrace = %v, want %v", c.ShutdownGrace, tc.wantShutdownGrace)
			}
		})
	}
}
