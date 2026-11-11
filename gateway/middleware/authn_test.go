package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func passthrough(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthnDisabledWithoutKey(t *testing.T) {
	response := httptest.NewRecorder()
	Authn("")(passthrough(t)).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/", nil))
	if response.Code != http.StatusOK {
		t.Errorf("no key must disable authn, got %d", response.Code)
	}
}

func TestAuthnRejectsMissingAndWrongKey(t *testing.T) {
	for name, header := range map[string]string{
		"missing": "",
		"wrong":   "nope",
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", nil)
			if header != "" {
				request.Header.Set(apiHeader, header)
			}
			response := httptest.NewRecorder()
			Authn("secret")(passthrough(t)).ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Errorf("code = %d, want 401", response.Code)
			}
			if decodeErrorBody(t, response.Body.Bytes()) != "unauthorized" {
				t.Errorf("body = %s", response.Body.String())
			}
		})
	}
}

func TestAuthnAcceptsCorrectKey(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Header.Set(apiHeader, "secret")
	response := httptest.NewRecorder()
	Authn("secret")(passthrough(t)).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Errorf("code = %d, want 200", response.Code)
	}
}
