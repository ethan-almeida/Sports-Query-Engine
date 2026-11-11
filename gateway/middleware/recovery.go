package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"sports-analytics/gateway/respond"
)

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Error("panic recovered",
						"request_id", w.Header().Get("X-Request-ID"),
						"panic", recovered,
						"stack", string(debug.Stack()),
					)
					respond.Error(w, http.StatusInternalServerError, "internal_error", "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
