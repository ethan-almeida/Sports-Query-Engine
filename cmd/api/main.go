package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"sportsql/cmd/api/ui"
	"sportsql/internal/cache"
	"sportsql/internal/config"
	"sportsql/internal/llm"
	"sportsql/internal/store"
)

func main() {
	settings := config.Load()
	if err := settings.ForAPI(); err != nil {
		slog.Error("config", "error", err.Error())
		os.Exit(1)
	}

	cacheStore, err := cache.New(settings.RedisURL)
	if err != nil {
		slog.Error("redis connection failed", "error", err.Error())
		os.Exit(1)
	}
	defer cacheStore.Close()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(settings.LogLevel),
	}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	roPool, err := connect(ctx, settings.DatabaseURLRO)
	if err != nil {
		logger.Error("database connection failed", "error", err.Error())
		os.Exit(1)
	}
	defer roPool.Close()
	executor := store.NewExecutor(roPool)
	deps := &askDeps{
		exec:        executor,
		llm:         llm.New(settings.LLMBaseURL, settings.LLMAPIKey),
		sqlModel:    settings.LLMModelSQL,
		answerModel: settings.LLMModelAnswer,
		cache:       cacheStore,
	}

	mux := http.NewServeMux()
	mux.Handle("GET /{$}", ui.Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("POST /ask", askHandler(deps))

	srv := &http.Server{
		Addr:              settings.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api starting", "addr", settings.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		logger.Error("api failed", "error", err.Error())
		os.Exit(1)
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		logger.Error("shutdown failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("api stopped")
}

// connect opens a pgx pool and verifies it with a ping.
func connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}

func parseLevel(level string) slog.Level {
	switch level {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
