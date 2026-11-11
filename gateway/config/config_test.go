package config

import (
	"testing"
	"time"
)

func setEnvs(t *testing.T, pairs map[string]string) {
	t.Helper()
	for key, value := range pairs {
		t.Setenv(key, value)
	}
}

func TestDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080", cfg.ListenAddr)
	}
	if cfg.OrchestratorURL != "http://localhost:8000" {
		t.Errorf("OrchestratorURL = %q", cfg.OrchestratorURL)
	}
	if cfg.OrchestratorTimeout != 20*time.Second {
		t.Errorf("OrchestratorTimeout = %v", cfg.OrchestratorTimeout)
	}
	if cfg.RateLimitPerMinute != 60 {
		t.Errorf("RateLimitPerMinute = %d", cfg.RateLimitPerMinute)
	}
	if cfg.LogLevel != "INFO" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.AuthnEnabled || cfg.CacheEnabled || cfg.RateLimitEnabled {
		t.Errorf("feature flags must be off with unset key/redis, got %+v", cfg)
	}
}

func TestOverrides(t *testing.T) {
	setEnvs(t, map[string]string{
		"GATEWAY_LISTEN_ADDR":           ":9090",
		"GATEWAY_ORCHESTRATOR_URL":      "http://orc.internal:8000/",
		"GATEWAY_ORCHESTRATOR_TIMEOUT":  "5",
		"GATEWAY_REDIS_URL":             "redis://localhost:6379/1",
		"GATEWAY_API_KEY":               "secret",
		"GATEWAY_RATE_LIMIT_PER_MINUTE": "10",
		"GATEWAY_LOG_LEVEL":             "DEBUG",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.OrchestratorURL != "http://orc.internal:8000" {
		t.Errorf("trailing slash must be trimmed, got %q", cfg.OrchestratorURL)
	}
	if cfg.OrchestratorTimeout != 5*time.Second {
		t.Errorf("OrchestratorTimeout = %v", cfg.OrchestratorTimeout)
	}
	if cfg.RateLimitPerMinute != 10 {
		t.Errorf("RateLimitPerMinute = %d", cfg.RateLimitPerMinute)
	}
	if cfg.LogLevel != "DEBUG" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if !cfg.AuthnEnabled || !cfg.CacheEnabled || !cfg.RateLimitEnabled {
		t.Errorf("feature flags must be on with key/redis set, got %+v", cfg)
	}
}

func TestInvalidValues(t *testing.T) {
	cases := map[string]map[string]string{
		"timeout":     {"GATEWAY_ORCHESTRATOR_TIMEOUT": "abc"},
		"neg timeout": {"GATEWAY_ORCHESTRATOR_TIMEOUT": "-3"},
		"rpm":         {"GATEWAY_RATE_LIMIT_PER_MINUTE": "0"},
		"rpm junk":    {"GATEWAY_RATE_LIMIT_PER_MINUTE": "x12"},
	}
	for name, envs := range cases {
		t.Run(name, func(t *testing.T) {
			for _, name := range []string{"GATEWAY_ORCHESTRATOR_TIMEOUT", "GATEWAY_RATE_LIMIT_PER_MINUTE"} {
				if _, ok := envs[name]; !ok {
					t.Setenv(name, "60")
				}
			}
			setEnvs(t, envs)
			if _, err := Load(); err == nil {
				t.Fatalf("expected error for %+v", envs)
			}
		})
	}
}
