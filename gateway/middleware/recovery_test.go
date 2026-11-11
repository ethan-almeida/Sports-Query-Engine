package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecoverTranslatesPanicTo500(t *testing.T) {
	panicking := passthrough(t)
	if panicking == nil {
		t.Fatal("unreachable")
	}

	response := httptest.NewRecorder()
	Recover(testLimiterLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("exploded")
	})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != 500 {
		t.Fatalf("code = %d, want 500", response.Code)
	}
	if decodeErrorBody(t, response.Body.Bytes()) != "internal_error" {
		t.Errorf("body = %s", response.Body.String())
	}
}

func TestRecoverPassesThroughNormalTraffic(t *testing.T) {
	response := httptest.NewRecorder()
	Recover(testLimiterLogger())(passthrough(t)).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != 200 {
		t.Errorf("code = %d", response.Code)
	}
}
