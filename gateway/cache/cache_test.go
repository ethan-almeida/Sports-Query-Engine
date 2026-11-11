package cache

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestBuildKeyDeterministic(t *testing.T) {
	first := BuildKey("nba", "top scorers?", 50)
	second := BuildKey("nba", "top scorers?", 50)
	if first != second {
		t.Errorf("same inputs produced different keys: %q vs %q", first, second)
	}
	if len(first) != 64 {
		t.Errorf("key length = %d, want 64 (sha256 hex)", len(first))
	}
	for _, char := range first {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			t.Errorf("key %q is not lower-case hex", first)
			break
		}
	}
}

func TestBuildKeyDistinguishesInputs(t *testing.T) {
	base := BuildKey("nba", "best team", 50)
	variations := map[string]string{
		"other sport":    BuildKey("nfl", "best team", 50),
		"other question": BuildKey("nba", "worst team", 50),
		"other rows":     BuildKey("nba", "best team", 100),
	}
	for name, value := range variations {
		if value == base {
			t.Errorf("%s must not collide with base key", name)
		}
	}
	if BuildKey("a|b", "c", 50) == BuildKey("a", "b|c", 50) {
		t.Errorf("separator collision is possible")
	}
}

func TestNoopAlwaysMisses(t *testing.T) {
	store := NoopStore{}
	value, hit, err := store.Get(context.Background(), "anything")
	if hit || err != nil || value != "" {
		t.Errorf("NoopStore must always miss, got (%q, %t, %v)", value, hit, err)
	}
}

func TestRedisStoreRoundTrip(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()

	store := NewClientStore(client)
	ctx := context.Background()

	if _, hit, err := store.Get(ctx, "missing"); err != nil || hit {
		t.Errorf("expected miss, got hit=%t err=%v", hit, err)
	}

	if err := client.Set(ctx, "payload", `{"query":"SELECT 1"}`, 0).Err(); err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	value, hit, err := store.Get(ctx, "payload")
	if err != nil || !hit || value != `{"query":"SELECT 1"}` {
		t.Errorf("expected hit with payload, got (%q, %t, %v)", value, hit, err)
	}
}

func TestRedisStoreReportsServerFailure(t *testing.T) {
	server := miniredis.RunT(t)
	listenAddr := server.Addr()
	server.Close()
	client := redis.NewClient(&redis.Options{Addr: listenAddr})
	defer func() { _ = client.Close() }()

	if _, _, err := NewClientStore(client).Get(context.Background(), "key"); err == nil {
		t.Fatalf("expected error against unreachable redis")
	}
}
