package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesEmbeddedPage(t *testing.T) {
	handler, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d", recorder.Code)
	}
	contentType := recorder.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		t.Errorf("content type = %q", contentType)
	}
	body := recorder.Body.String()
	for _, marker := range []string{`id="ask"`, `id="sql_drawer"`, "/api/v1/query"} {
		if !strings.Contains(body, marker) {
			t.Errorf("page is missing %q", marker)
		}
	}
}
