package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"sports-analytics/gateway/model"
)

const maxResponseBytes = 16 << 20

var (
	ErrUnavailable = errors.New("orchestrator unavailable")
	ErrTimeout     = errors.New("orchestrator timeout")
)

type Outcome struct {
	Status   int
	Response model.QueryResponse
	Err      *model.Error
}

type Client interface {
	Orchestrate(ctx context.Context, request model.OrchestrateRequest) (Outcome, error)
}

type HTTPClient struct {
	base    string
	timeout time.Duration
	client  *http.Client
}

func New(base string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		base:    strings.TrimRight(base, "/"),
		timeout: timeout,
		client: &http.Client{
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				ResponseHeaderTimeout: timeout,
			},
		},
	}
}

func (client *HTTPClient) Orchestrate(ctx context.Context, request model.OrchestrateRequest) (Outcome, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return Outcome{}, fmt.Errorf("%w: encode request: %w", ErrUnavailable, err)
	}

	callCtx, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()

	httpRequest, err := http.NewRequestWithContext(callCtx, http.MethodPost, client.base+"/v1/orchestrate", bytes.NewReader(payload))
	if err != nil {
		return Outcome{}, fmt.Errorf("%w: build request: %w", ErrUnavailable, err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")

	httpResponse, err := client.client.Do(httpRequest)
	if err != nil {
		var networkTimeout net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkTimeout) && networkTimeout.Timeout()) {
			return Outcome{}, ErrTimeout
		}
		return Outcome{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = httpResponse.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, maxResponseBytes))
	if err != nil {
		return Outcome{}, fmt.Errorf("%w: read response: %w", ErrUnavailable, err)
	}

	return decode(httpResponse.StatusCode, body)
}

func decode(status int, body []byte) (Outcome, error) {
	if status != http.StatusOK {
		envelope, err := parseError(body)
		if err != nil {
			return Outcome{}, fmt.Errorf("%w: non-JSON failure body (status %d)", ErrUnavailable, status)
		}
		return Outcome{Status: status, Err: envelope}, nil
	}

	response, err := parseResponse(body)
	if err != nil {
		return Outcome{}, fmt.Errorf("%w: non-JSON success body", ErrUnavailable)
	}
	return Outcome{Status: status, Response: response}, nil
}

func parseError(body []byte) (*model.Error, error) {
	var envelope struct {
		Error *model.ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error == nil || envelope.Error.Code == "" {
		return nil, fmt.Errorf("not an error envelope")
	}
	return &model.Error{Error: *envelope.Error}, nil
}

func parseResponse(body []byte) (model.QueryResponse, error) {
	var response model.QueryResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return model.QueryResponse{}, err
	}
	return response, nil
}
