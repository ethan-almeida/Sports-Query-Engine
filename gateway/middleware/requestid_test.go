package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDGenerated(t *testing.T) {
	var headerID, contextID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headerID = w.Header().Get("X-Request-ID")
		contextID = RequestIDFrom(r.Context())
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if len(contextID) != 32 {
		t.Errorf("generated id length = %d, want 32", len(contextID))
	}
	if headerID != contextID {
		t.Errorf("header and context id diverge: %q vs %q", headerID, contextID)
	}
}

func TestRequestIDEchoed(t *testing.T) {
	var captured string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = RequestIDFrom(r.Context())
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "trace-1239")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if captured != "trace-1239" {
		t.Errorf("id = %q, want client-provided", captured)
	}
}

func TestRequestIDRejectedWhenUnsafe(t *testing.T) {
	cases := map[string]string{
		"too long":     string(bytes.Repeat([]byte("x"), 65)),
		"control char": "bad\x1fid",
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			var captured string
			handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured = RequestIDFrom(r.Context())
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("X-Request-ID", value)
			handler.ServeHTTP(httptest.NewRecorder(), request)

			if captured == value || len(captured) != 32 {
				t.Errorf("unsafe id must be replaced, got %q", captured)
			}
		})
	}
}

func TestRemoteIdentityPrefersXFF(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "10.0.0.9:5555"
	request.Header.Set("X-Forwarded-For", "203.0.113.5, 70.41.3.25")
	if got := remoteIdentity(request); got != "203.0.113.5" {
		t.Errorf("remoteIdentity = %q", got)
	}

	request.Header.Del("X-Forwarded-For")
	if got := remoteIdentity(request); got != "10.0.0.9" {
		t.Errorf("remoteIdentity = %q", got)
	}
}
