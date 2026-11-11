package handler

import (
	"context"
	"net/http"
	"time"

	"sports-analytics/gateway/respond"
)

type Health struct {
	redisPING func(context.Context) error
}

func NewHealth(ping func(context.Context) error) *Health {
	return &Health{redisPING: ping}
}

func (handler *Health) Healthz(w http.ResponseWriter, r *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (handler *Health) Readyz(w http.ResponseWriter, r *http.Request) {
	if handler.redisPING == nil {
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := handler.redisPING(ctx); err != nil {
		respond.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unready"})
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
