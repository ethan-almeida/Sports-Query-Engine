package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Settings struct {
	ListenAddr      string
	LogLevel        string
	LLMBaseURL      string
	LLMAPIKey       string
	LLMModelSQL     string
	LLMModelAnswer  string
	DatabaseURLRO   string
	DatabaseURLRW   string
	RedisURL        string
	ESPNBaseURL     string
	ESPNCoreBaseURL string
}

func Load() *Settings {
	file := parseEnvFile(findEnvFile())
	return &Settings{
		ListenAddr:      lookup("LISTEN_ADDR", ":8080", file),
		LogLevel:        lookup("LOG_LEVEL", "INFO", file),
		LLMBaseURL:      lookup("LLM_BASE_URL", "", file),
		LLMAPIKey:       lookup("LLM_API_KEY", "", file),
		LLMModelSQL:     lookup("LLM_MODEL_SQL", "", file),
		LLMModelAnswer:  lookup("LLM_MODEL_ANSWER", "", file),
		DatabaseURLRO:   lookup("DATABASE_URL_RO", "", file),
		DatabaseURLRW:   lookup("DATABASE_URL_RW", "", file),
		RedisURL:        lookup("REDIS_URL", "", file),
		ESPNBaseURL:     lookup("ESPN_BASE_URL", "https://site.api.espn.com", file),
		ESPNCoreBaseURL: lookup("ESPN_CORE_BASE_URL", "https://sports.core.api.espn.com", file),
	}
}

func (s *Settings) ForAPI() error {
	var missing []string
	check := func(name, val string) {
		if val == "" {
			missing = append(missing, name)
		}
	}
	check("LLM_BASE_URL", s.LLMBaseURL)
	check("LLM_API_KEY", s.LLMAPIKey)
	check("LLM_MODEL_SQL", s.LLMModelSQL)
	check("LLM_MODEL_ANSWER", s.LLMModelAnswer)
	check("DATABASE_URL_RO", s.DatabaseURLRO)
	check("REDIS_URL", s.RedisURL)
	if len(missing) > 0 {
		return fmt.Errorf("config: missing required for api: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (s *Settings) ForIngest() error {
	var missing []string
	check := func(name, val string) {
		if val == "" {
			missing = append(missing, name)
		}
	}
	check("DATABASE_URL_RW", s.DatabaseURLRW)
	if len(missing) > 0 {
		return fmt.Errorf("config: missing required for ingest: %s", strings.Join(missing, ", "))
	}
	return nil
}

func lookup(key, def string, file map[string]string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	if v, ok := file[key]; ok && v != "" {
		return v
	}
	return def
}

func findEnvFile() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func parseEnvFile(path string) map[string]string {
	out := map[string]string{}
	if path == "" {
		return out
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = stripQuotes(strings.TrimSpace(val))
	}
	return out
}

func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
