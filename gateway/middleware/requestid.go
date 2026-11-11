package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
)

type requestIDContextKey struct{}

const maxRequestIDLen = 64

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeID(r.Header.Get("X-Request-ID"))
		if id == "" {
			id = generateID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, id)))
	})
}

func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDContextKey{}).(string); ok {
		return id
	}
	return ""
}

func sanitizeID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > maxRequestIDLen {
		return ""
	}
	for _, char := range value {
		if char < ' ' || char > '~' {
			return ""
		}
	}
	return value
}

func generateID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(raw)
}

func remoteIdentity(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first := strings.TrimSpace(strings.Split(forwarded, ",")[0]); first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
