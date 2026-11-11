package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"sports-analytics/ingestor/config"
	"sports-analytics/ingestor/extract"
	"sports-analytics/ingestor/ingest"
	"sports-analytics/ingestor/sources"
	"sports-analytics/ingestor/store"
	"sports-analytics/ingestor/watermark"
)

const usage = "usage: ingestor [serve|once|seed]"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	mode := ingest.ModeOnce
	if len(args) > 0 {
		switch ingest.Mode(args[0]) {
		case ingest.ModeServe, ingest.ModeOnce, ingest.ModeSeed:
			mode = ingest.Mode(args[0])
		default:
			fmt.Fprintln(os.Stderr, usage)
			return 2
		}
	}

	settings, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		return 2
	}
	logger := newLogger(settings.LogLevel)
	slog.SetDefault(logger)
	logger.Info("ingestor config",
		"mode", mode, "database", redactCredentials(settings.DatabaseURL),
		"sources_config", settings.SourcesConfig, "daemon_interval", settings.DaemonInterval.String())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connect(ctx, settings.DatabaseURL)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		return 1
	}
	defer pool.Close()

	persistence := store.New(pool)
	water := watermark.New(pool)
	registry, err := extract.LoadSources(settings.SourcesConfig, sources.DefaultRegistry(sources.Options{
		TheSportsDBAPIKey:  settings.TheSportsDBAPIKey,
		TheSportsDBBaseURL: settings.TheSportsDBBaseURL,
		ESPNBaseURL:        settings.ESPNBaseURL,
		ESPNCoreBaseURL:    settings.ESPNCoreBaseURL,
	}))
	if err != nil {
		logger.Error("loading sources failed", "error", err)
		return 2
	}
	runner := ingest.New(logger, persistence, water, extract.RetryOptions{
		MaxAttempts: 4,
		BaseDelay:   200 * time.Millisecond,
		MaxDelay:    2 * time.Second,
		Sleep:       time.Sleep,
	})

	switch mode {
	case ingest.ModeOnce, ingest.ModeSeed:
		if mode == ingest.ModeSeed {
			logger.Info("seed run ignores stored watermarks and rebuilds from the first page")
		}
		total, err := runAll(ctx, logger, runner, registry, mode)
		if err != nil {
			logger.Error("ingest run failed", "mode", mode, "error", err)
			return 1
		}
		logger.Info("ingest run finished", "mode", mode, "sources", len(registry), "rows", total)
		return 0
	default:
		return serve(ctx, logger, runner, registry, settings.DaemonInterval)
	}
}

func runAll(ctx context.Context, logger *slog.Logger, runner *ingest.Runner, sourceList []extract.Source, mode ingest.Mode) (int, error) {
	total := 0
	for _, source := range sourceList {
		select {
		case <-ctx.Done():
			return total, fmt.Errorf("interrupted: %w", ctx.Err())
		default:
		}
		rows, err := runner.RunSource(ctx, source, mode)
		if err != nil {
			return total, err
		}
		logger.Info("source finished", "source", source.Def().ID, "mode", mode, "rows", rows)
		total += rows
	}
	return total, nil
}

func serve(ctx context.Context, logger *slog.Logger, runner *ingest.Runner, sourceList []extract.Source, interval time.Duration) int {
	logger.Info("daemon start", "sources", len(sourceList), "interval", interval.String())
	if _, err := runAll(ctx, logger, runner, sourceList, ingest.ModeOnce); err != nil {
		logger.Error("ingest cycle failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("daemon stop")
			return 0
		case <-ticker.C:
		}
		if _, err := runAll(ctx, logger, runner, sourceList, ingest.ModeOnce); err != nil {
			logger.Error("ingest cycle failed", "error", err)
		}
		if ctx.Err() != nil {
			logger.Info("daemon stop")
			return 0
		}
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

func connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("connect: timed out: %w", err)
		}
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}

func redactCredentials(databaseURL string) string {
	schemeIndex := strings.Index(databaseURL, "://")
	atIndex := strings.LastIndex(databaseURL, "@")
	if schemeIndex < 0 || atIndex < schemeIndex {
		return databaseURL
	}
	return databaseURL[:schemeIndex+3] + "<redacted>" + databaseURL[atIndex:]
}
