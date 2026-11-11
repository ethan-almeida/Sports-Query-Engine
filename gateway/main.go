package main

import (
	"log/slog"
	"os"
	"strings"

	"sports-analytics/gateway/config"
	"sports-analytics/gateway/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config error", "error", err.Error())
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)

	if err := server.Run(cfg, logger); err != nil {
		logger.Error("gateway failed", "error", err.Error())
		os.Exit(1)
	}
}

func newLogger(level string) *slog.Logger {
	var parsed slog.Level
	switch strings.ToUpper(level) {
	case "DEBUG":
		parsed = slog.LevelDebug
	case "WARN":
		parsed = slog.LevelWarn
	case "ERROR":
		parsed = slog.LevelError
	default:
		parsed = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parsed}))
}
