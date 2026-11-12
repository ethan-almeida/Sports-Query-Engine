// Package cache provides the Redis-backed answer cache.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/redis/go-redis/v9"
)

// how long a stored result lives.
const TTL = 6 * time.Hour

// Result is the body stored in Redis for one question.
type Result struct {
	Answer   string   `json:"answer"`
	SQL      string   `json:"sql"`
	Rows     [][]any  `json:"rows"`
	Columns  []string `json:"columns"`
	DataAsOf string   `json:"data_as_of"`
}

// Store caches finished answers keyed by normalized question + date.
type Store struct {
	rdb *redis.Client
}

// New connects to Redis and verifies it with a ping.
func New(url string) (*Store, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("cache: parse redis url: %w", err)
	}
	rdb := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("cache: ping: %w", err)
	}
	return &Store{rdb: rdb}, nil
}

// Close releases the underlying connection pool.
func (s *Store) Close() error {
	return s.rdb.Close()
}

// Get returns the stored result for a question on the given date, and whether
// it was a hit.
func (s *Store) Get(ctx context.Context, question, date string) (Result, bool, error) {
	raw, err := s.rdb.Get(ctx, Key(date, question)).Bytes()
	if err == redis.Nil {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, fmt.Errorf("cache: get: %w", err)
	}
	var res Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return Result{}, false, fmt.Errorf("cache: decode: %w", err)
	}
	return res, true, nil
}

// Set stores a finished result under the question's key for TTL.
func (s *Store) Set(ctx context.Context, question, date string, res Result) error {
	raw, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("cache: encode: %w", err)
	}
	if err := s.rdb.Set(ctx, Key(date, question), raw, TTL).Err(); err != nil {
		return fmt.Errorf("cache: set: %w", err)
	}
	return nil
}

// Key builds the cache key for a question and date.
func Key(date, question string) string {
	return "ask:" + date + ":" + Normalize(question)
}

// Normalize lowercases a question, drops punctuation, and collapses runs of
// whitespace to a single space.
func Normalize(question string) string {
	var b strings.Builder
	inSpace := false
	for _, r := range strings.ToLower(question) {
		if unicode.IsSpace(r) {
			inSpace = true
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if inSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			inSpace = false
		}
	}
	return b.String()
}
