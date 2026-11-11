package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sports-analytics/gateway/model"
)

const validResponse = `{"query":"SELECT 1","columns":["a"],"rows":[[1]],"row_count":1,"truncated":false,"elapsed_ms":3}`

func TestSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/orchestrate" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing content type")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(validResponse))
	}))
	defer server.Close()

	outcome, err := New(server.URL, 5*time.Second).Orchestrate(context.Background(), model.OrchestrateRequest{
		Sport:    "nba",
		Question: "q",
		MaxRows:  50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != http.StatusOK || outcome.Err != nil {
		t.Fatalf("unexpected outcome %+v", outcome)
	}
	if outcome.Response.Query != "SELECT 1" || outcome.Response.RowCount != 1 || outcome.Response.ElapsedMS != 3 {
		t.Errorf("bad response %+v", outcome.Response)
	}
	if outcome.Response.Rows[0][0] != float64(1) {
		t.Errorf("rows not parsed: %+v", outcome.Response.Rows)
	}
}

func TestErrorEnvelopePassthrough(t *testing.T) {
	body := `{"error":{"code":"safety_violation","message":"no safe query"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	outcome, err := New(server.URL, 5*time.Second).Orchestrate(context.Background(), model.OrchestrateRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d", outcome.Status)
	}
	if outcome.Err == nil || outcome.Err.Error.Code != "safety_violation" || outcome.Err.Error.Message != "no safe query" {
		t.Fatalf("envelope not preserved: %+v", outcome.Err)
	}
}

func TestNonJSONFailureBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("<html>boom</html>"))
	}))
	defer server.Close()

	_, err := New(server.URL, 5*time.Second).Orchestrate(context.Background(), model.OrchestrateRequest{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestGarbageSuccessBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("nope"))
	}))
	defer server.Close()

	_, err := New(server.URL, 5*time.Second).Orchestrate(context.Background(), model.OrchestrateRequest{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestConnectionRefused(t *testing.T) {
	_, err := New("http://127.0.0.1:1", 5*time.Second).Orchestrate(context.Background(), model.OrchestrateRequest{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
}

func TestHardTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(validResponse))
	}))
	defer server.Close()

	started := time.Now()
	_, err := New(server.URL, 50*time.Millisecond).Orchestrate(context.Background(), model.OrchestrateRequest{})

	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
	if time.Since(started) > time.Second {
		t.Errorf("timeout must be per call, took %v", time.Since(started))
	}
	if !strings.Contains(err.Error(), "orchestrator timeout") {
		t.Errorf("error text = %q", err.Error())
	}
}

func TestCallerContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := New(server.URL, 5*time.Second).Orchestrate(ctx, model.OrchestrateRequest{})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("caller cancellation must surface as ErrTimeout, got %v", err)
	}
}
