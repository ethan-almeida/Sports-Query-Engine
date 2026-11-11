package espn

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

const SourceType = "espn"

const (
	defaultSiteBaseURL  = "https://site.api.espn.com"
	defaultCoreBaseURL  = "https://sports.core.api.espn.com"
	defaultPaceInterval = 350 * time.Millisecond
	penaltyWait         = 20 * time.Second
	maxResponseBytes    = 8 << 20
)

type Options struct {
	SiteBaseURL string
	CoreBaseURL string
}

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
	siteBase string
	coreBase string
	interval time.Duration
	wait     sleeper
	mu       sync.Mutex
	last     time.Time
}

func newClient(options Options) *client {
	return newClientWithWait(options, defaultPaceInterval, sleepWait)
}

func newClientWithWait(options Options, interval time.Duration, wait sleeper) *client {
	siteBase := options.SiteBaseURL
	if siteBase == "" {
		siteBase = defaultSiteBaseURL
	}
	coreBase := options.CoreBaseURL
	if coreBase == "" {
		coreBase = defaultCoreBaseURL
	}
	return &client{
		http:     &http.Client{Timeout: 15 * time.Second},
		siteBase: strings.TrimRight(siteBase, "/"),
		coreBase: strings.TrimRight(coreBase, "/"),
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

func (c *client) fetchSiteJSON(ctx context.Context, path string, params url.Values, out any) error {
	if err := c.fetchJSON(ctx, c.siteBase, path, params, out); err != nil {
		return fmt.Errorf("espn site %s: %w", path, err)
	}
	return nil
}

func (c *client) fetchCoreJSON(ctx context.Context, path string, out any) error {
	if err := c.fetchJSON(ctx, c.coreBase, path, nil, out); err != nil {
		return fmt.Errorf("espn core %s: %w", path, err)
	}
	return nil
}

func (c *client) fetchJSON(ctx context.Context, base, path string, params url.Values, out any) error {
	if err := c.pace(ctx); err != nil {
		return fmt.Errorf("pace %s: %w", path, err)
	}
	if err := c.get(ctx, base, path, params, out); err != nil {
		return err
	}
	return nil
}

func (c *client) get(ctx context.Context, base, path string, params url.Values, out any) error {
	body, status, err := c.request(ctx, base, path, params)
	if err != nil {
		return err
	}
	if status == http.StatusTooManyRequests {
		if err := c.wait(ctx, penaltyWait); err != nil {
			return fmt.Errorf("rate-limit penalty interrupted: %w", err)
		}
		if err := c.pace(ctx); err != nil {
			return fmt.Errorf("pace %s: %w", path, err)
		}
		body, status, err = c.request(ctx, base, path, params)
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

func (c *client) request(ctx context.Context, base, path string, params url.Values) ([]byte, int, error) {
	target := base + "/" + strings.TrimLeft(path, "/")
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
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
	trimmed := strings.TrimSpace(string(body))
	if cut := strings.IndexByte(trimmed, '\n'); cut >= 0 {
		trimmed = trimmed[:cut]
	}
	if len(trimmed) > 200 {
		trimmed = trimmed[:200]
	}
	return trimmed
}
