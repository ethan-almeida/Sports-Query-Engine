package middleware

import (
	"crypto/subtle"
	"net/http"

	"sports-analytics/gateway/respond"
)

const apiHeader = "X-API-Key"

func Authn(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !constantTimeEqual(r.Header.Get(apiHeader), key) {
				respond.Error(w, http.StatusUnauthorized, "unauthorized", "missing or invalid API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func constantTimeEqual(actual, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}
