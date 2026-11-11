package middleware

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func limiterClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func TestRateLimitAllowsWithinLimit(t *testing.T) {
	_, client := limiterClient(t)
	next := passthrough(t)
	handler := RateLimit(client, 3, testLimiterLogger())(next)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/query", nil)
	request.RemoteAddr = "203.0.113.9:1234"

	for range 3 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("request %d blocked with %d", 1, response.Code)
		}
	}
}

func TestRateLimitBlocksBeyondLimit(t *testing.T) {
	_, client := limiterClient(t)
	handler := RateLimit(client, 2, testLimiterLogger())(passthrough(t))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/query", nil)
	request.RemoteAddr = "203.0.113.9:1234"

	for range 2 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("early request blocked with %d", response.Code)
		}
	}

	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, request)
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("code = %d, want 429", blocked.Code)
	}
	if decodeErrorBody(t, blocked.Body.Bytes()) != "rate_limited" {
		t.Errorf("body = %s", blocked.Body.String())
	}
	if _, err := time.ParseDuration(blocked.Header().Get("Retry-After") + "s"); err != nil {
		t.Errorf("Retry-After = %q", blocked.Header().Get("Retry-After"))
	}
}

func TestRateLimitResetsNextWindow(t *testing.T) {
	_, client := limiterClient(t)
	handler := RateLimit(client, 1, testLimiterLogger())(passthrough(t))

	request := httptest.NewRequest(http.MethodPost, "/", nil)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, request)
	if first.Code != http.StatusOK {
		t.Fatalf("first request blocked")
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, request)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request in the same window must be blocked, code = %d", second.Code)
	}

	restore := swapClock(windowNow().Add(61 * time.Second))
	defer restore()
	third := httptest.NewRecorder()
	handler.ServeHTTP(third, request)
	if third.Code != http.StatusOK {
		t.Fatalf("request after window must pass, code = %d", third.Code)
	}
}

func swapClock(future time.Time) func() {
	previous := windowNow
	windowNow = func() time.Time { return future }
	return func() { windowNow = previous }
}

func TestRateLimitKeysByAPIKey(t *testing.T) {
	_, client := limiterClient(t)
	handler := RateLimit(client, 1, testLimiterLogger())(passthrough(t))

	alpha := httptest.NewRequest(http.MethodPost, "/", nil)
	alpha.Header.Set(apiHeader, "alpha-key")
	beta := httptest.NewRequest(http.MethodPost, "/", nil)
	beta.Header.Set(apiHeader, "beta-key")

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, alpha)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, alpha)

	if first.Code != http.StatusOK || second.Code != http.StatusTooManyRequests {
		t.Fatalf("same key must share a window: %d, %d", first.Code, second.Code)
	}

	third := httptest.NewRecorder()
	handler.ServeHTTP(third, beta)
	if third.Code != http.StatusOK {
		t.Fatalf("distinct key must have its own window, code = %d", third.Code)
	}
}

func TestRateLimitFailsOpenOnRedisOutage(t *testing.T) {
	server := miniredis.RunT(t)
	listenAddr := server.Addr()
	server.Close()
	client := redis.NewClient(&redis.Options{
		Addr:       listenAddr,
		MaxRetries: -1,
		Dialer: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 300 * time.Millisecond}).DialContext(ctx, network, address)
		},
	})
	defer func() { _ = client.Close() }()

	handler := RateLimit(client, 1, testLimiterLogger())(passthrough(t))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("limiter outage must fail open, code = %d", response.Code)
	}
}

func TestRateLimitWithoutRedisPassesThrough(t *testing.T) {
	handler := RateLimit(nil, 1, testLimiterLogger())(passthrough(t))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusOK {
		t.Errorf("nil client must disable limiting, code = %d", response.Code)
	}
}
