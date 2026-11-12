// Package llm is a minimal OpenAI-compatible chat client over plain HTTP.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Usage is the token accounting reported by the provider for one call.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Message is one chat message in the request.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client talks to one OpenAI-compatible endpoint.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New builds a Client. baseURL is the chat completions root.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 90 * time.Second},
	}
}

// Chat sends the messages with the named model and returns the assistant text.
// Token usage from the response is always logged. A single 429 is retried after
// the retry-after delay when the provider sends one.
func (c *Client) Chat(ctx context.Context, model string, messages []Message) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":    model,
		"messages": messages,
	})
	if err != nil {
		return "", fmt.Errorf("llm: encode request: %w", err)
	}
	url := c.baseURL + "/chat/completions"
	text, retryAfter, err := c.doOnce(ctx, url, body, model)
	if err == err429 && retryAfter > 0 {
		wait := retryAfter
		if wait > 30*time.Second {
			wait = 30 * time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
		}
		text, _, err = c.doOnce(ctx, url, body, model)
	}
	return text, err
}

var err429 = fmt.Errorf("llm: rate limited")

// doOnce performs a single chat request. It returns the reply text, a
// retry-after duration (when present), and any error.
func (c *Client) doOnce(ctx context.Context, url string, body []byte, model string) (string, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("llm: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("llm: request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", 0, fmt.Errorf("llm: read response: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return "", parseRetryAfter(resp.Header.Get("Retry-After")), err429
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", 0, fmt.Errorf("llm: status %d: %s", resp.StatusCode, truncate(string(data), 300))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", 0, fmt.Errorf("llm: decode response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", 0, fmt.Errorf("llm: empty choices in response")
	}
	slog.Info("llm call",
		"model", model,
		"prompt_tokens", parsed.Usage.PromptTokens,
		"completion_tokens", parsed.Usage.CompletionTokens,
		"total_tokens", parsed.Usage.TotalTokens,
	)
	return parsed.Choices[0].Message.Content, 0, nil
}

// parseRetryAfter reads a Retry-After header as seconds or an HTTP date.
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 2 * time.Second
	}
	if secs, err := strconv.Atoi(header); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(header); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 2 * time.Second
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
