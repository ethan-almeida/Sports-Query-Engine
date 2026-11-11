package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr          string
	OrchestratorURL     string
	OrchestratorTimeout time.Duration
	RedisURL            string
	APIKey              string
	RateLimitPerMinute  int
	LogLevel            string

	AuthnEnabled     bool
	CacheEnabled     bool
	RateLimitEnabled bool
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
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

func envInt(key string, fallback int) (int, error) {
	raw := env(key, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("config: %s must be a positive integer, got %q", key, raw)
	}
	return value, nil
}

func Load() (Config, error) {
	timeout, err := envSeconds("GATEWAY_ORCHESTRATOR_TIMEOUT", 20*time.Second)
	if err != nil {
		return Config{}, err
	}
	rateLimit, err := envInt("GATEWAY_RATE_LIMIT_PER_MINUTE", 60)
	if err != nil {
		return Config{}, err
	}

	redisURL := env("GATEWAY_REDIS_URL", "")
	apiKey := env("GATEWAY_API_KEY", "")

	return Config{
		ListenAddr:          env("GATEWAY_LISTEN_ADDR", ":8080"),
		OrchestratorURL:     strings.TrimRight(env("GATEWAY_ORCHESTRATOR_URL", "http://localhost:8000"), "/"),
		OrchestratorTimeout: timeout,
		RedisURL:            redisURL,
		APIKey:              apiKey,
		RateLimitPerMinute:  rateLimit,
		LogLevel:            env("GATEWAY_LOG_LEVEL", "INFO"),

		AuthnEnabled:     apiKey != "",
		CacheEnabled:     redisURL != "",
		RateLimitEnabled: redisURL != "",
	}, nil
}
