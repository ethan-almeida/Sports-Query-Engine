package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"sports-analytics/gateway/cache"
	"sports-analytics/gateway/model"
	"sports-analytics/gateway/orchestrator"
)

const storedPayload = `{
  "query": "SELECT team, pts FROM sports.games WHERE league = 'nba'",
  "columns": ["team", "pts", "active"],
  "rows": [["Lakers", 118.5, true]],
  "row_count": 1,
  "truncated": false,
  "elapsed_ms": 12
}`

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(discard{}, nil))
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func postQuery(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/query", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestCacheFirstFlowMissThenHit(t *testing.T) {
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = redisClient.Close() }()

	var orchestratorCalls int
	const sport = "nba"
	const question = "which team scored the most points?"

	fakeOrchestrator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orchestratorCalls++
		var received model.OrchestrateRequest
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("orchestrator got unparseable body: %v", err)
		}
		if received.CacheKey != cache.BuildKey(sport, question, 50) {
			t.Errorf("cache_key %q does not match sha256(sport||question||max_rows)", received.CacheKey)
		}
		if received.Sport != sport || received.Question != question || received.MaxRows != 50 {
			t.Errorf("request drifted: %+v", received)
		}
		if err := redisClient.Set(context.Background(), received.CacheKey, storedPayload, 300*time.Second).Err(); err != nil {
			t.Errorf("staging payload failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(storedPayload))
	}))
	defer fakeOrchestrator.Close()

	query := handlerFor(t, cache.NewClientStore(redisClient), fakeOrchestrator.URL)

	first := postQuery(t, query, `{"sport":"nba","question":"which team scored the most points?"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first query code = %d, body = %s", first.Code, first.Body.String())
	}
	if cacheState := first.Header().Get("X-Cache"); cacheState != "MISS" {
		t.Errorf("first X-Cache = %q", cacheState)
	}
	var response model.QueryResponse
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatalf("response not JSON: %s", first.Body.String())
	}
	if response.Query == "" || response.RowCount != 1 || response.Columns[0] != "team" || response.Rows[0][1] != 118.5 {
		t.Errorf("response drift: %+v", response)
	}
	if orchestratorCalls != 1 {
		t.Fatalf("orchestrator called %d times on miss", orchestratorCalls)
	}

	if _, err := server.Get(cache.BuildKey(sport, question, 50)); err != nil {
		t.Fatalf("orchestrator staged the response under the gateway key, but the key is absent: %v", err)
	}

	fakeOrchestrator.Close()

	cached := postQuery(t, query, `{"sport":"nba","question":"which team scored the most points?"}`)
	if cached.Code != http.StatusOK {
		t.Fatalf("cached query code = %d", cached.Code)
	}
	if cacheState := cached.Header().Get("X-Cache"); cacheState != "HIT" {
		t.Errorf("second X-Cache = %q, want HIT", cacheState)
	}
	if orchestratorCalls != 1 {
		t.Errorf("orchestrator called %d times total; hit must not touch it", orchestratorCalls)
	}
	if !strings.Contains(cached.Body.String(), "SELECT team, pts") {
		t.Errorf("hit body must be the stored payload, got %s", cached.Body.String())
	}
}

func handlerFor(t *testing.T, store cache.ResponseStore, orchestratorURL string) http.Handler {
	t.Helper()
	return NewQuery(store, orchestrator.New(orchestratorURL, 5*time.Second), testLogger())
}

func TestInvalidBodyRejected(t *testing.T) {
	query := handlerFor(t, cache.NoopStore{}, "http://127.0.0.1:1")

	cases := map[string]string{
		"not json":        "nonsense",
		"missing sport":   `{"question":"q"}`,
		"missing field":   `{}`,
		"rows out of rge": `{"sport":"nba","question":"q","max_rows":1001}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			response := postQuery(t, query, body)
			if response.Code != http.StatusBadRequest {
				t.Errorf("code = %d, want 400", response.Code)
			}
			if code := errorCode(t, response.Body.Bytes()); code != "bad_request" {
				t.Errorf("error code = %q", code)
			}
		})
	}
}

func TestOrchestratorErrorProxiedVerbatim(t *testing.T) {
	fakeOrchestrator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"safety_violation","message":"no safe query"}}`))
	}))
	defer fakeOrchestrator.Close()

	response := postQuery(t, handlerFor(t, cache.NoopStore{}, fakeOrchestrator.URL), `{"sport":"nba","question":"drop table users"}`)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422", response.Code)
	}
	if code := errorCode(t, response.Body.Bytes()); code != "safety_violation" {
		t.Errorf("code = %q", code)
	}
	if response.Header().Get("X-Cache") != "" {
		t.Errorf("failures must not carry X-Cache")
	}
}

func TestUpstreamTimeoutBecomes504(t *testing.T) {
	fakeOrchestrator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
	}))
	defer fakeOrchestrator.Close()

	query := NewQuery(cache.NoopStore{}, orchestrator.New(fakeOrchestrator.URL, 40*time.Millisecond), testLogger())
	response := postQuery(t, query, `{"sport":"nba","question":"q"}`)
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("code = %d, want 504", response.Code)
	}
	if code := errorCode(t, response.Body.Bytes()); code != "orchestrator_timeout" {
		t.Errorf("code = %q", code)
	}
}

func TestDownstreamDownBecomes502(t *testing.T) {
	query := handlerFor(t, cache.NoopStore{}, "http://127.0.0.1:1")
	response := postQuery(t, query, `{"sport":"nba","question":"q"}`)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want 502", response.Code)
	}
	if code := errorCode(t, response.Body.Bytes()); code != "orchestrator_unavailable" {
		t.Errorf("code = %q", code)
	}
}

func TestNoStoreAlwaysMisses(t *testing.T) {
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = redisClient.Close() }()

	staged := cache.BuildKey("nba", "q", 50)
	if err := redisClient.Set(context.Background(), staged, storedPayload, time.Minute).Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fakeOrchestrator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(storedPayload))
	}))
	defer fakeOrchestrator.Close()

	response := postQuery(t, handlerFor(t, cache.NoopStore{}, fakeOrchestrator.URL), `{"sport":"nba","question":"q"}`)
	if response.Header().Get("X-Cache") != "MISS" {
		t.Errorf("X-Cache = %q, want MISS without a store", response.Header().Get("X-Cache"))
	}
}

func TestUndecodableCachedPayloadFallsBackToMiss(t *testing.T) {
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = redisClient.Close() }()

	staged := cache.BuildKey("nba", "q", 50)
	if err := redisClient.Set(context.Background(), staged, "definitely not json", time.Minute).Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fakeOrchestrator := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(storedPayload))
	}))
	defer fakeOrchestrator.Close()

	response := postQuery(t, handlerFor(t, cache.NewClientStore(redisClient), fakeOrchestrator.URL), `{"sport":"nba","question":"q"}`)
	if response.Code != http.StatusOK || response.Header().Get("X-Cache") != "MISS" {
		t.Errorf("garbage payload must degrade to miss, got %d %q", response.Code, response.Header().Get("X-Cache"))
	}
}

func errorCode(t *testing.T, raw []byte) string {
	t.Helper()
	var envelope model.Error
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("body not an error envelope: %s", raw)
	}
	return envelope.Error.Code
}
