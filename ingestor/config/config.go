package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL        string
	SourcesConfig      string
	DaemonInterval     time.Duration
	LogLevel           string
	TheSportsDBAPIKey  string
	TheSportsDBBaseURL string
	ESPNBaseURL        string
	ESPNCoreBaseURL    string
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func Load() (Config, error) {
	interval, err := envSeconds("INGESTOR_DAEMON_INTERVAL_SECONDS", 300*time.Second)
	if err != nil {
		return Config{}, err
	}
	sourcesConfig := env("INGESTOR_SOURCES_CONFIG", "")
	if sourcesConfig == "" {
		located, err := locateSourcesConfig()
		if err != nil {
			return Config{}, err
		}
		sourcesConfig = located
	}
	return Config{
		DatabaseURL:        env("INGESTOR_DATABASE_URL", "postgres://ingestor:dev_ingestor@localhost:5432/sports"),
		SourcesConfig:      sourcesConfig,
		DaemonInterval:     interval,
		LogLevel:           env("INGESTOR_LOG_LEVEL", "INFO"),
		TheSportsDBAPIKey:  env("INGESTOR_THESPORTSDB_API_KEY", "123"),
		TheSportsDBBaseURL: env("INGESTOR_THESPORTSDB_BASE_URL", "https://www.thesportsdb.com/api/v1/json"),
		ESPNBaseURL:        env("INGESTOR_ESPN_BASE_URL", "https://site.api.espn.com"),
		ESPNCoreBaseURL:    env("INGESTOR_ESPN_CORE_BASE_URL", "https://sports.core.api.espn.com"),
	}, nil
}

func locateSourcesConfig() (string, error) {
	candidates := []string{"configs/sources.yaml", "../configs/sources.yaml"}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("config: INGESTOR_SOURCES_CONFIG not set and no sources.yaml found in %v", candidates)
}

func envSeconds(key string, fallback time.Duration) (time.Duration, error) {
	raw := env(key, "")
	if raw == "" {
		return fallback, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("config: %s must be a positive integer of seconds, got %q", key, raw)
	}
	return time.Duration(seconds) * time.Second, nil
}
