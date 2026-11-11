package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"sports-analytics/gateway/cache"
	"sports-analytics/gateway/middleware"
	"sports-analytics/gateway/model"
	"sports-analytics/gateway/orchestrator"
	"sports-analytics/gateway/respond"
)

const maxBodyBytes = 1 << 20

type Query struct {
	store  cache.ResponseStore
	client orchestrator.Client
	logger *slog.Logger
}

func NewQuery(store cache.ResponseStore, client orchestrator.Client, logger *slog.Logger) *Query {
	return &Query{store: store, client: client, logger: logger}
}

func (handler *Query) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := readBody(w, r)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "request body is invalid")
		return
	}

	request, err := model.NewRequest(raw)
	if err != nil {
		handler.logger.Warn("invalid query request", "error", err.Error(),
			"request_id", middleware.RequestIDFrom(r.Context()))
		respond.Error(w, http.StatusBadRequest, "bad_request", "request body is invalid")
		return
	}

	if hit, ok := handler.cached(r, request); ok {
		w.Header().Set("X-Cache", "HIT")
		respond.JSON(w, http.StatusOK, hit)
		return
	}

	outcome, err := handler.client.Orchestrate(r.Context(), model.OrchestrateRequest{
		Sport:    request.Sport,
		Question: request.Question,
		MaxRows:  request.MaxRows,
		CacheKey: cache.BuildKey(request.Sport, request.Question, request.MaxRows),
	})
	switch {
	case errors.Is(err, orchestrator.ErrTimeout):
		respond.Error(w, http.StatusGatewayTimeout, "orchestrator_timeout", "the orchestrator timed out")
		return
	case errors.Is(err, orchestrator.ErrUnavailable):
		handler.logger.Error("orchestrator call failed", "error", err.Error(),
			"request_id", middleware.RequestIDFrom(r.Context()))
		respond.Error(w, http.StatusBadGateway, "orchestrator_unavailable", "the orchestrator is unavailable")
		return
	case err != nil:
		handler.logger.Error("orchestrator call failed", "error", err.Error(),
			"request_id", middleware.RequestIDFrom(r.Context()))
		respond.Error(w, http.StatusBadGateway, "orchestrator_unavailable", "the orchestrator is unavailable")
		return
	case outcome.Err != nil:
		respond.JSON(w, respond.UpstreamStatus(outcome.Status), *outcome.Err)
		return
	}

	w.Header().Set("X-Cache", "MISS")
	respond.JSON(w, http.StatusOK, outcome.Response)
}

func (handler *Query) cached(r *http.Request, request model.QueryRequest) (model.QueryResponse, bool) {
	if handler.store == nil {
		return model.QueryResponse{}, false
	}
	value, hit, err := handler.store.Get(r.Context(), cache.BuildKey(request.Sport, request.Question, request.MaxRows))
	if err == nil && hit {
		response, decodeErr := decodeStored(value)
		if decodeErr == nil {
			return response, true
		}
		handler.logger.Warn("stored cache payload is undecodable, treating as miss",
			"error", decodeErr.Error(), "request_id", middleware.RequestIDFrom(r.Context()))
	}
	return model.QueryResponse{}, false
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer func() { _ = body.Close() }()
	return io.ReadAll(body)
}

func decodeStored(value string) (model.QueryResponse, error) {
	var response model.QueryResponse
	if err := json.NewDecoder(strings.NewReader(value)).Decode(&response); err != nil {
		return model.QueryResponse{}, err
	}
	if response.IsZero() {
		return model.QueryResponse{}, errors.New("empty stored payload")
	}
	return response, nil
}
