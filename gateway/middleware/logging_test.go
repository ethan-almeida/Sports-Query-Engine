package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingCapturesStatusAndFields(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buffer, nil))
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("closed"))
	})

	chain := RequestID(Logging(logger)(inner))
	chain.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/query", nil))

	var record map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &record); err != nil {
		t.Fatalf("log line is not JSON: %s", buffer.String())
	}
	if record["status"] != float64(http.StatusTeapot) {
		t.Errorf("status = %v", record["status"])
	}
	if record["method"] != "POST" || record["path"] != "/api/v1/query" {
		t.Errorf("method/path = %v/%v", record["method"], record["path"])
	}
	if id, ok := record["request_id"].(string); !ok || len(id) != 32 {
		t.Errorf("request_id = %v", record["request_id"])
	}
	if _, ok := record["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms missing: %v", record)
	}

	buffer.Reset()
	inner500 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	RequestID(Logging(logger)(inner500)).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if buffer.Len() == 0 {
		t.Fatalf("no log emitted")
	}
}

func TestLevelForMapsStatusToLevel(t *testing.T) {
	if levelFor(200) != slog.LevelInfo {
		t.Errorf("200 must be info")
	}
	if levelFor(404) != slog.LevelWarn {
		t.Errorf("4xx must be warn")
	}
	if levelFor(500) != slog.LevelError {
		t.Errorf("5xx must be error")
	}
}
