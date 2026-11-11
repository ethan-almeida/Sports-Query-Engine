package espn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Options configures the ESPN endpoints. Empty values fall back to the public
// defaults.
type Options struct {
	// SiteBaseURL hosts the JSON scoreboard API.
	SiteBaseURL string
	// WebBaseURL hosts the SSR box-score pages.
	WebBaseURL string
	// HTTPClient may be injected for tests.
	HTTPClient *http.Client
}

const (
	defaultSiteBase = "https://site.api.espn.com"
	defaultWebBase  = "https://www.espn.com"

	defaultPaceInterval = 350 * time.Millisecond
	// retryPenalty is the backoff after a 429 or a WAF (202) challenge.
	retryPenalty     = 2 * time.Minute
	maxAttempts      = 3
	maxResponseBytes = 8 << 20
	// userAgent identifies the fetcher. ESPN's front serves SSR pages to
	// browser-like clients and challenges bare clients with a JS wall.
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
)

type sleeper func(ctx context.Context, d time.Duration) error

func realSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// client paces ESPN requests and backs off on rate limits and WAF challenges.
type client struct {
	http     *http.Client
	siteBase string
	webBase  string
	interval time.Duration
	penalty  time.Duration
	wait     sleeper
	log      *slog.Logger

	mu   sync.Mutex
	last time.Time
}

func newClient(options Options) *client {
	siteBase := strings.TrimRight(options.SiteBaseURL, "/")
	if siteBase == "" {
		siteBase = defaultSiteBase
	}
	webBase := strings.TrimRight(options.WebBaseURL, "/")
	if webBase == "" {
		webBase = defaultWebBase
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &client{
		http:     httpClient,
		siteBase: siteBase,
		webBase:  webBase,
		interval: defaultPaceInterval,
		penalty:  retryPenalty,
		wait:     realSleep,
		log:      slog.Default(),
	}
}

// newTestClient builds a client with a short penalty and injectable sleep so
// tests never really wait. It is used only by tests.
func newTestClient(options Options, wait sleeper) *client {
	c := newClient(options)
	c.penalty = 5 * time.Millisecond
	c.wait = wait
	return c
}

// pace enforces a minimum interval between requests.
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

// get performs a GET with pacing, backoff-and-retry on 429 / WAF-202, and a
// body size cap. It returns the response body for status 200.
func (c *client) get(ctx context.Context, target string) ([]byte, error) {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			c.log.Warn("espn retry after backoff", "attempt", attempt, "url", target)
		}
		if err := c.pace(ctx); err != nil {
			return nil, err
		}
		body, status, challenge, err := c.doOnce(ctx, target)
		if err != nil {
			// Network-level error: retry after backoff unless we are out of attempts.
			if attempt >= maxAttempts {
				return nil, err
			}
			if err := c.wait(ctx, c.penalty); err != nil {
				return nil, err
			}
			continue
		}
		switch {
		case status == http.StatusOK && !challenge:
			return body, nil
		case status == http.StatusTooManyRequests || challenge:
			reason := "rate limited (429)"
			if challenge {
				reason = "WAF challenge (202)"
			}
			c.log.Warn("espn backing off", "url", target, "reason", reason, "penalty", c.penalty.String())
			if attempt >= maxAttempts {
				if challenge {
					return nil, fmt.Errorf("%w: espn %s is still behind the WAF challenge after %d attempts; wait for the rate limit to clear or retry from a residential connection", ErrWAFChallenge, short(target), maxAttempts)
				}
				return nil, fmt.Errorf("espn %s: still %s after %d attempts", short(target), reason, maxAttempts)
			}
			if err := c.wait(ctx, c.penalty); err != nil {
				return nil, err
			}
		default:
			return nil, &httpError{status: status, detail: firstLine(body)}
		}
	}
	return nil, fmt.Errorf("espn %s: gave up after %d attempts", short(target), maxAttempts)
}

// doOnce performs a single GET. It returns the body, the status, and whether
// the response is a WAF challenge (202 with a small JS-challenge stub).
func (c *client) doOnce(ctx context.Context, target string) (body []byte, status int, challenge bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/html, */*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, false, fmt.Errorf("do %s: %w", short(target), err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, false, fmt.Errorf("read body (status %d): %w", resp.StatusCode, err)
	}
	return body, resp.StatusCode, isWAFChallenge(resp.StatusCode, body), nil
}

// isWAFChallenge detects the AWS WAF JS-challenge response: an HTTP 202 (or a
// 200 with a very small body) whose payload is a challenge stub, not a page.
func isWAFChallenge(status int, body []byte) bool {
	if status == http.StatusAccepted {
		return true
	}
	if status != http.StatusOK {
		return false
	}
	return len(body) < 5000 && strings.Contains(string(body), "waf")
}

// ErrWAFChallenge is returned when ESPN's WAF keeps serving a JS challenge and
// the request cannot be completed. It is safe to retry later once the
// rate-limit state clears.
var ErrWAFChallenge = errors.New("espn waf challenge")

func short(target string) string {
	if i := strings.Index(target, "?"); i > 0 {
		return target[:i]
	}
	return target
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

type httpError struct {
	status int
	detail string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("espn status %d: %s", e.status, e.detail)
}

// IsHTTPError unwraps a non-retryable HTTP error from err, if any.
func IsHTTPError(err error) *httpError {
	var he *httpError
	if errors.As(err, &he) {
		return he
	}
	return nil
}
