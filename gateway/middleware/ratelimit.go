package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"sports-analytics/gateway/respond"
)

const windowSeconds = 60

var windowNow = func() time.Time { return time.Now() }

func RateLimit(client *redis.Client, limit int, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if client == nil {
				next.ServeHTTP(w, r)
				return
			}

			now := windowNow()
			windowKey := fmt.Sprintf("gw:ratelimit:%s:%010d", identity(r), now.Unix()/windowSeconds)
			pipe := client.TxPipeline()
			countCmd := pipe.Incr(r.Context(), windowKey)
			pipe.Expire(r.Context(), windowKey, 2*windowSeconds*time.Second)
			if _, err := pipe.Exec(r.Context()); err != nil {
				logger.Error("rate limiter unavailable, failing open", "error", err)
				next.ServeHTTP(w, r)
				return
			}

			if countCmd.Val() > int64(limit) {
				w.Header().Set("Retry-After", strconv.Itoa(windowSeconds-int(now.Unix()%windowSeconds)))
				respond.Error(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func identity(r *http.Request) string {
	if key := r.Header.Get(apiHeader); key != "" {
		return key
	}
	return remoteIdentity(r)
}
