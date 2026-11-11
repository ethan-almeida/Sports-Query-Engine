package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"sports-analytics/gateway/config"
	"sports-analytics/gateway/model"
)

const storedPayload = `{"query":"SELECT 1","columns":["n"],"rows":[[1]],"row_count":1,"truncated":false,"elapsed_ms":1}`

func TestEndToEndFlowAndGracefulShutdown(t *testing.T) {
	server := miniredis.RunT(t)
	stagingClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = stagingClient.Close() }()

	fakeOrchestrator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var received model.OrchestrateRequest
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("fake orchestrator: %v", err)
		}
		if err := stagingClient.Set(context.Background(), received.CacheKey, storedPayload, time.Minute).Err(); err != nil {
			t.Errorf("fake orchestrator staging: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(storedPayload))
	}))
	defer fakeOrchestrator.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("closing probe listener: %v", err)
	}

	cfg := config.Config{
		ListenAddr:          addr,
		OrchestratorURL:     fakeOrchestrator.URL,
		OrchestratorTimeout: 5 * time.Second,
		RedisURL:            "redis://" + server.Addr() + "/0",
		RateLimitPerMinute:  60,
		LogLevel:            "INFO",
		CacheEnabled:        true,
		RateLimitEnabled:    true,
	}

	done := make(chan error, 1)
	go func() {
		done <- Run(cfg, slog.New(slog.NewJSONHandler(discard{}, nil)))
	}()

	base := "http://" + addr
	if err := waitHealthy(base+"/healthz", 10*time.Second); err != nil {
		t.Fatalf("gateway never came up: %v", err)
	}

	body := strings.NewReader(`{"sport":"nba","question":"which team won?"}`)
	first, err := http.Post(base+"/api/v1/query", "application/json", body)
	if err != nil {
		t.Fatalf("first query: %v", err)
	}
	assertCacheHeader(t, first, "MISS")
	defer func() { _ = first.Body.Close() }()

	cachedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/query", strings.NewReader(`{"sport":"nba","question":"which team won?"}`))
	_ = cachedRequest
	second, err := http.Post(base+"/api/v1/query", "application/json", strings.NewReader(`{"sport":"nba","question":"which team won?"}`))
	if err != nil {
		t.Fatalf("second query: %v", err)
	}
	assertCacheHeader(t, second, "HIT")
	defer func() { _ = second.Body.Close() }()

	uiResponse, err := http.Get(base + "/")
	if err != nil {
		t.Fatalf("ui: %v", err)
	}
	if uiResponse.StatusCode != http.StatusOK {
		t.Errorf("ui status = %d", uiResponse.StatusCode)
	}
	if contentType := uiResponse.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Errorf("ui content type = %q", contentType)
	}
	uiBody, err := io.ReadAll(uiResponse.Body)
	_ = uiResponse.Body.Close()
	if err != nil {
		t.Fatalf("ui body: %v", err)
	}
	if !strings.Contains(string(uiBody), `id="ask"`) {
		t.Errorf("ui body is missing the form marker")
	}

	postRoot, err := http.Post(base+"/", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post root: %v", err)
	}
	_ = postRoot.Body.Close()
	if postRoot.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("post root status = %d, want %d", postRoot.StatusCode, http.StatusMethodNotAllowed)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("sending SIGTERM: %v", err)
	}
	select {
	case runErr := <-done:
		if runErr != nil {
			t.Errorf("Run returned error: %v", runErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Run did not shut down gracefully")
	}
}

func assertCacheHeader(t *testing.T, response *http.Response, want string) {
	t.Helper()
	if response.StatusCode != http.StatusOK {
		t.Errorf("status = %d", response.StatusCode)
		return
	}
	if got := response.Header.Get("X-Cache"); got != want {
		t.Errorf("X-Cache = %q, want %q", got, want)
	}
	var parsed model.QueryResponse
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		t.Errorf("response not JSON: %v", err)
	}
}

func waitHealthy(url string, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			if response.StatusCode == http.StatusOK {
				_ = response.Body.Close()
				return nil
			}
			_ = response.Body.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("healthz unreachable within %v", budget)
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
