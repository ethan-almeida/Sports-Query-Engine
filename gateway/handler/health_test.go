package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestHealthzAlwaysOK(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewHealth(nil).Healthz(recorder, httptest.NewRequest("GET", "/healthz", nil))
	if recorder.Code != 200 {
		t.Errorf("code = %d", recorder.Code)
	}
	if body := recorder.Body.String(); body[1] != '"' || len(body) == 0 {
		t.Errorf("body = %s", body)
	}
}

func TestReadyzHealthy(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewHealth(func(context.Context) error { return nil }).Readyz(recorder, httptest.NewRequest("GET", "/readyz", nil))
	if recorder.Code != 200 {
		t.Errorf("code = %d", recorder.Code)
	}
}

func TestReadyzUnreadyOnDependencyFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewHealth(func(context.Context) error { return errors.New("redis down") }).Readyz(recorder, httptest.NewRequest("GET", "/readyz", nil))
	if recorder.Code != 503 {
		t.Errorf("code = %d", recorder.Code)
	}
}

func TestReadyzReadyWithoutRedis(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewHealth(nil).Readyz(recorder, httptest.NewRequest("GET", "/readyz", nil))
	if recorder.Code != 200 {
		t.Errorf("code = %d", recorder.Code)
	}
}
