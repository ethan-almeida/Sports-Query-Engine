package tsdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"sports-analytics/ingestor/extract"
)

const SourceType = "thesportsdb"

const (
	defaultPaceInterval = 2200 * time.Millisecond
	penaltyWait         = 65 * time.Second
	maxResponseBytes    = 8 << 20
)

type Options struct {
	APIKey  string
	BaseURL string
}

const defaultBaseURL = "https://www.thesportsdb.com/api/v1/json"
const defaultFreeAPIKey = "123"

type sleeper func(ctx context.Context, d time.Duration) error

func sleepWait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type client struct {
	http     *http.Client
	apiKey   string
	baseURL  string
	interval time.Duration
	wait     sleeper
	mu       sync.Mutex
	last     time.Time
}

func newClient(options Options) *client {
	return newClientWithWait(options, defaultPaceInterval, sleepWait)
}

func newClientWithWait(options Options, interval time.Duration, wait sleeper) *client {
	baseURL := options.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = defaultFreeAPIKey
	}
	return &client{
		http:     &http.Client{Timeout: 15 * time.Second},
		apiKey:   apiKey,
		baseURL:  strings.TrimRight(baseURL, "/"),
		interval: interval,
		wait:     wait,
	}
}

func (c *client) pace(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	earliest := c.last.Add(c.interval)
	if c.last.IsZero() || !earliest.After(now) {
		c.last = now
		c.mu.Unlock()
		return nil
	}
	c.last = earliest
	delay := earliest.Sub(now)
	c.mu.Unlock()
	return c.wait(ctx, delay)
}

func (c *client) fetchJSON(ctx context.Context, endpoint string, params url.Values, out any) error {
	if err := c.pace(ctx); err != nil {
		return fmt.Errorf("tsdb: pace %s: %w", endpoint, err)
	}
	if err := c.get(ctx, endpoint, params, out); err != nil {
		return fmt.Errorf("tsdb: %s: %w", endpoint, err)
	}
	return nil
}

func (c *client) get(ctx context.Context, endpoint string, params url.Values, out any) error {
	body, status, err := c.request(ctx, endpoint, params)
	if err != nil {
		return err
	}
	if status == http.StatusTooManyRequests {
		if err := c.wait(ctx, penaltyWait); err != nil {
			return fmt.Errorf("rate-limit penalty interrupted: %w", err)
		}
		if err := c.pace(ctx); err != nil {
			return fmt.Errorf("tsdb: pace: %w", err)
		}
		body, status, err = c.request(ctx, endpoint, params)
		if err != nil {
			return err
		}
		if status == http.StatusTooManyRequests {
			return fmt.Errorf("still rate limited after %s penalty: status 429", penaltyWait)
		}
	}
	if status >= 500 {
		return fmt.Errorf("status %d: %s", status, firstLine(body))
	}
	if status < 200 || status >= 300 {
		return &extract.UnretryableError{Err: fmt.Errorf("status %d: %s", status, firstLine(body))}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *client) request(ctx context.Context, endpoint string, params url.Values) ([]byte, int, error) {
	url := strings.Join([]string{c.baseURL, c.apiKey, endpoint}, "/")
	if len(params) > 0 {
		url += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, &extract.UnretryableError{Err: fmt.Errorf("build request: %w", err)}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("do: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read body (status %d): %w", resp.StatusCode, err)
	}
	return body, resp.StatusCode, nil
}

func firstLine(body []byte) string {
	trim := strings.TrimSpace(string(body))
	if cut := strings.IndexByte(trim, '\n'); cut >= 0 {
		trim = trim[:cut]
	}
	if len(trim) > 200 {
		trim = trim[:200]
	}
	return trim
}
