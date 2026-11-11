package extract

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"
)

type RetryOptions struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Sleep       func(time.Duration)
}

func Retry(ctx context.Context, logger *slog.Logger, options RetryOptions, operation string, fn func(ctx context.Context) error) error {
	var lastErr error
	for attempt := 1; ; attempt++ {
		if err := fn(ctx); err == nil {
			if attempt > 1 {
				logger.Info("retry succeeded", "operation", operation, "attempt", attempt)
			}
			return nil
		} else {
			lastErr = err
			if !IsRetryable(err) || ctx.Err() != nil || attempt >= options.MaxAttempts {
				break
			}
			delay := jitter(delayFor(options, attempt))
			logger.Warn("operation failed, retrying",
				"operation", operation, "attempt", attempt,
				"max_attempts", options.MaxAttempts,
				"next_delay_ms", delay.Milliseconds(), "error", err)
			options.Sleep(delay)
		}
	}
	if lastErr == nil {
		return fmt.Errorf("retry: no attempts made for %s", operation)
	}
	return fmt.Errorf("%s: %w", operation, lastErr)
}

type UnretryableError struct {
	Err error
}

func (e *UnretryableError) Error() string { return fmt.Sprintf("unretryable: %v", e.Err) }
func (e *UnretryableError) Unwrap() error { return e.Err }

func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var unretryable *UnretryableError
	return !errors.As(err, &unretryable)
}

func delayFor(options RetryOptions, attempt int) time.Duration {
	growth := time.Duration(1<<uint(attempt-1)) * options.BaseDelay
	if growth > options.MaxDelay {
		growth = options.MaxDelay
	}
	return growth
}

func jitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(delay) + 1))
}
