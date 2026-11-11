package config

import "testing"

func TestDefaults(t *testing.T) {
	t.Setenv("INGESTOR_SOURCES_CONFIG", "configs/sources.yaml")
	t.Setenv("INGESTOR_DATABASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DaemonInterval.Seconds() != 300 {
		t.Errorf("DaemonInterval = %v", cfg.DaemonInterval)
	}
	if cfg.LogLevel != "INFO" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.DatabaseURL == "" {
		t.Errorf("database url default missing")
	}
	if cfg.SourcesConfig != "configs/sources.yaml" {
		t.Errorf("SourcesConfig = %q", cfg.SourcesConfig)
	}
}

func TestOverrideAndInvalid(t *testing.T) {
	t.Setenv("INGESTOR_SOURCES_CONFIG", "alternate.yaml")
	t.Setenv("INGESTOR_DAEMON_INTERVAL_SECONDS", "7")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DaemonInterval.Seconds() != 7 {
		t.Errorf("DaemonInterval = %v", cfg.DaemonInterval)
	}
	if cfg.SourcesConfig != "alternate.yaml" {
		t.Errorf("SourcesConfig = %q", cfg.SourcesConfig)
	}

	t.Setenv("INGESTOR_DAEMON_INTERVAL_SECONDS", "-2")
	if _, err := Load(); err == nil {
		t.Errorf("negative interval must fail")
	}
	t.Setenv("INGESTOR_DAEMON_INTERVAL_SECONDS", "junk")
	if _, err := Load(); err == nil {
		t.Errorf("junk interval must fail")
	}
}

func TestTheSportsDBDefaultsAndOverrides(t *testing.T) {
	t.Setenv("INGESTOR_SOURCES_CONFIG", "closed-then-later.yaml")
	t.Setenv("INGESTOR_THESPORTSDB_API_KEY", "")
	t.Setenv("INGESTOR_THESPORTSDB_BASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TheSportsDBAPIKey != "123" {
		t.Errorf("TheSportsDBAPIKey default = %q", cfg.TheSportsDBAPIKey)
	}
	if cfg.TheSportsDBBaseURL != "https://www.thesportsdb.com/api/v1/json" {
		t.Errorf("TheSportsDBBaseURL default = %q", cfg.TheSportsDBBaseURL)
	}

	t.Setenv("INGESTOR_THESPORTSDB_API_KEY", "  secret ")
	t.Setenv("INGESTOR_THESPORTSDB_BASE_URL", " http://localhost:9999/json ")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TheSportsDBAPIKey != "secret" {
		t.Errorf("TheSportsDBAPIKey override = %q", cfg.TheSportsDBAPIKey)
	}
	if cfg.TheSportsDBBaseURL != "http://localhost:9999/json" {
		t.Errorf("TheSportsDBBaseURL override = %q", cfg.TheSportsDBBaseURL)
	}
}

func TestMissingSourcesConfigFails(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("INGESTOR_SOURCES_CONFIG", "")
	if _, err := Load(); err == nil {
		t.Errorf("missing sources config must fail when no default exists")
	}
}
