package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const unitSeparator = "\x1f"

type ResponseStore interface {
	Get(ctx context.Context, key string) (string, bool, error)
}

type NoopStore struct{}

func (NoopStore) Get(context.Context, string) (string, bool, error) {
	return "", false, nil
}

type RedisStore struct {
	client *redis.Client
}

func NewClientStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

func (store *RedisStore) Get(ctx context.Context, key string) (string, bool, error) {
	value, err := store.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func BuildKey(sport string, question string, maxRows int) string {
	digest := sha256.Sum256([]byte(sport + unitSeparator + question + unitSeparator + strconv.Itoa(maxRows)))
	return hex.EncodeToString(digest[:])
}
