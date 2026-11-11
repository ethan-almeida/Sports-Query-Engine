package extract

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type sleepRecord struct {
	deltas []time.Duration
}

func (s *sleepRecord) sleep(d time.Duration) {
	s.deltas = append(s.deltas, d)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(testWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
}

type testWriter struct{}

func (testWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestRetrySucceedsAfterTransientFailures(t *testing.T) {
	record := &sleepRecord{}
	options := RetryOptions{MaxAttempts: 4, BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second, Sleep: record.sleep}
	attempts := 0
	err := Retry(context.Background(), newTestLogger(), options, "demo", func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("transient boom")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if len(record.deltas) != 2 {
		t.Fatalf("deltas = %v, want 2 sleeps", record.deltas)
	}
	if record.deltas[0] > 100*time.Millisecond {
		t.Errorf("first delta %v exceeds base bound", record.deltas[0])
	}
	if record.deltas[1] > 200*time.Millisecond {
		t.Errorf("second delta %v exceeds doubled bound", record.deltas[1])
	}
}

func TestRetryExhaustsAttemptsAndWrapsLastError(t *testing.T) {
	record := &sleepRecord{}
	options := RetryOptions{MaxAttempts: 3, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond, Sleep: record.sleep}
	attempts := 0
	err := Retry(context.Background(), newTestLogger(), options, "demo", func(ctx context.Context) error {
		attempts++
		return errors.New("always broken")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if len(record.deltas) != 2 {
		t.Errorf("sleeps = %v, want 2 (no sleep after final attempt)", record.deltas)
	}
	if !strings.Contains(err.Error(), "always broken") {
		t.Errorf("error %v must wrap cause", err)
	}
}

func TestRetryStopsImmediatelyOnUnretryable(t *testing.T) {
	record := &sleepRecord{}
	options := RetryOptions{MaxAttempts: 5, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond, Sleep: record.sleep}
	attempts := 0
	err := Retry(context.Background(), newTestLogger(), options, "demo", func(ctx context.Context) error {
		attempts++
		return &UnretryableError{Err: errors.New("bad shape")}
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if len(record.deltas) != 0 {
		t.Errorf("sleeps = %v, want none", record.deltas)
	}
}

func TestRetryHonorsContextCancellation(t *testing.T) {
	record := &sleepRecord{}
	options := RetryOptions{MaxAttempts: 5, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond, Sleep: record.sleep}
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := Retry(ctx, newTestLogger(), options, "demo", func(c context.Context) error {
		attempts++
		if attempts == 1 {
			cancel()
		}
		return errors.New("boom")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (canceled inside the failing call)", attempts)
	}
}

func TestIsRetryableContextErrors(t *testing.T) {
	if IsRetryable(nil) {
		t.Errorf("nil must not be retryable")
	}
	if IsRetryable(context.DeadlineExceeded) {
		t.Errorf("deadline exceeded must be classified unretryable")
	}
}
