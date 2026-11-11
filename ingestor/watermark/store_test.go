package watermark

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("INGESTOR_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("integration test requires INGESTOR_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("skipping integration test, database unreachable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test, database ping failed: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestWatermarkRoundTrip(t *testing.T) {
	pool := testPool(t)
	water := New(pool)
	ctx := context.Background()
	streamSuffix := time.Now().UnixNano()
	sourceID := fmt.Sprintf("synthetic-test-%d", streamSuffix)
	stream := "snapshot-roundtrip"

	if _, err := water.Get(ctx, sourceID, stream); err != nil {
		t.Fatalf("get absent: %v", err)
	}

	wanted := map[string]any{"page": 2, "note": "mid-batch"}
	if err := water.Save(ctx, sourceID, stream, wanted); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := water.Get(ctx, sourceID, stream)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got["page"] != float64(2) {
		t.Errorf("page = %v(%T), want float64(2)", got["page"], got["page"])
	}
	if got["note"] != "mid-batch" {
		t.Errorf("note = %v, want mid-batch", got["note"])
	}

	if err := water.Save(ctx, sourceID, stream, map[string]any{"page": float64(4)}); err != nil {
		t.Fatalf("save overwrite: %v", err)
	}
	got, err = water.Get(ctx, sourceID, stream)
	if err != nil {
		t.Fatalf("get overwrite: %v", err)
	}
	if got["page"] != float64(4) {
		t.Errorf("page = %v, want 4 after overwrite", got["page"])
	}
	if saved, err := water.Get(ctx, sourceID+"-absent", stream); err != nil || saved != nil {
		t.Errorf("absent watermark = %v, %v; want nil, nil", saved, err)
	}
}
