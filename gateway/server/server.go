package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"sports-analytics/gateway/cache"
	"sports-analytics/gateway/config"
	"sports-analytics/gateway/handler"
	"sports-analytics/gateway/middleware"
	"sports-analytics/gateway/orchestrator"
	"sports-analytics/gateway/ui"
)

const shutdownGrace = 10 * time.Second

func Run(cfg config.Config, logger *slog.Logger) error {
	redisClient, err := connectRedis(cfg.RedisURL, logger)
	if err != nil {
		return err
	}
	if redisClient != nil {
		defer func() { _ = redisClient.Close() }()
	}

	dependencyPing := func(ctx context.Context) error {
		if redisClient == nil {
			return nil
		}
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return redisClient.Ping(pingCtx).Err()
	}

	responseStore := buildStore(redisClient)
	upstream := orchestrator.New(cfg.OrchestratorURL, cfg.OrchestratorTimeout)
	queryHandler := handler.NewQuery(responseStore, upstream, logger)
	healthHandler := handler.NewHealth(dependencyPing)

	uiHandler, err := ui.Handler()
	if err != nil {
		return fmt.Errorf("load ui: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /{$}", uiHandler)
	mux.HandleFunc("GET /healthz", healthHandler.Healthz)
	mux.HandleFunc("GET /readyz", healthHandler.Readyz)
	mux.Handle("POST /api/v1/query", middleware.Authn(cfg.APIKey)(middleware.RateLimit(redisClient, cfg.RateLimitPerMinute, logger)(queryHandler)))

	root := middleware.Recover(logger)(middleware.RequestID(middleware.Logging(logger)(mux)))

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("gateway starting",
		"addr", cfg.ListenAddr,
		"orchestrator", cfg.OrchestratorURL,
		"orchestrator_timeout", cfg.OrchestratorTimeout.String(),
		"authn_enabled", cfg.AuthnEnabled,
		"cache_enabled", cfg.CacheEnabled,
		"rate_limit_per_minute", cfg.RateLimitPerMinute,
		"rate_limit_enabled", cfg.RateLimitEnabled,
	)

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- httpServer.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case sig := <-stop:
		logger.Info("shutdown signal received", "signal", sig.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("gateway stopped")
	return nil
}

func buildStore(client *redis.Client) cache.ResponseStore {
	if client == nil {
		return cache.NoopStore{}
	}
	return cache.NewClientStore(client)
}

func connectRedis(url string, logger *slog.Logger) (*redis.Client, error) {
	if url == "" {
		return nil, nil
	}
	options, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(options)
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	logger.Info("redis connected", "url", url)
	return client, nil
}
