package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"sports-analytics/ingestor/extract"
	"sports-analytics/ingestor/model"
	"sports-analytics/ingestor/store"
	"sports-analytics/ingestor/watermark"
)

func newRunner(t *testing.T, pool *pgxpool.Pool) *Runner {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	options := extract.RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   5 * time.Millisecond,
		MaxDelay:    20 * time.Millisecond,
		Sleep:       time.Sleep,
	}
	return New(logger, store.New(pool), watermark.New(pool), options)
}

func runnerPool(t *testing.T) *pgxpool.Pool {
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

type page struct {
	rows  []model.Row
	done  bool
	error error
}

type scriptedSource struct {
	def     extract.SourceDef
	streams map[string][]page
}

func (s *scriptedSource) Def() extract.SourceDef { return s.def }

func (s *scriptedSource) Fetch(ctx context.Context, stream string, cursor map[string]any) (extract.Batch, error) {
	pages, ok := s.streams[stream]
	if !ok {
		return extract.Batch{}, &extract.UnretryableError{Err: fmt.Errorf("no stream %q", stream)}
	}
	current := 0
	if raw, present := cursor["page"]; present {
		switch value := raw.(type) {
		case int:
			current = value
		case float64:
			if value != float64(int(value)) {
				return extract.Batch{}, &extract.UnretryableError{Err: fmt.Errorf("page %v must be whole", value)}
			}
			current = int(value)
		default:
			return extract.Batch{}, &extract.UnretryableError{Err: fmt.Errorf("page %T is not numeric", raw)}
		}
	}
	if current >= len(pages) {
		return extract.Batch{Cursor: map[string]any{"page": current}, Done: true}, nil
	}
	step := pages[current]
	if step.error != nil {
		return extract.Batch{}, step.error
	}
	return extract.Batch{
		Stream: stream,
		Cursor: map[string]any{"page": current + 1},
		Rows:   step.rows,
		Done:   step.done,
	}, nil
}

func leagueRow(code string) model.Row {
	return model.Row{Kind: model.KindLeague, League: &model.League{Code: code, Name: "League " + code, Sport: "synthetic"}}
}

func TestRunnerPersistsWatermarkPerBatch(t *testing.T) {
	pool := runnerPool(t)
	water := watermark.New(pool)
	ctx := context.Background()
	sourceID := fmt.Sprintf("scripted-%d", time.Now().UnixNano())

	source := &scriptedSource{
		def: extract.SourceDef{ID: sourceID, Type: "synthetic", Streams: []string{"snap"}},
		streams: map[string][]page{
			"snap": {
				{rows: []model.Row{leagueRow("SCRATCH-A")}},
				{rows: []model.Row{leagueRow("SCRATCH-B")}, done: true},
			},
		},
	}
	runner := newRunner(t, pool)
	rows, err := runner.RunSource(ctx, source, ModeOnce)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rows != 2 {
		t.Errorf("rows = %d, want 2", rows)
	}
	saved, err := water.Get(ctx, sourceID, "snap")
	if err != nil {
		t.Fatalf("get watermark: %v", err)
	}
	if saved == nil || saved["page"] != float64(2) {
		t.Errorf("final watermark = %v, want page 2", saved)
	}
}

func TestRunnerResumesAfterMidBatchFailure(t *testing.T) {
	pool := runnerPool(t)
	water := watermark.New(pool)
	ctx := context.Background()
	sourceID := fmt.Sprintf("scripted-resume-%d", time.Now().UnixNano())

	failure := errors.New("simulated crash after first batch")
	first := &scriptedSource{
		def: extract.SourceDef{ID: sourceID, Type: "synthetic", Streams: []string{"snap"}},
		streams: map[string][]page{
			"snap": {
				{rows: []model.Row{leagueRow("SCRATCH-R1")}},
				{error: failure},
			},
		},
	}
	runner := newRunner(t, pool)
	_, err := runner.RunSource(ctx, first, ModeOnce)
	if err == nil || !errors.Is(err, failure) {
		t.Fatalf("expected propagated failure, got %v", err)
	}

	after, err := water.Get(ctx, sourceID, "snap")
	if err != nil {
		t.Fatalf("get watermark after crash: %v", err)
	}
	if after == nil || after["page"] != float64(1) {
		t.Fatalf("watermark after crash = %v, want page 1 (second batch not saved)", after)
	}

	repaired := &scriptedSource{
		def: extract.SourceDef{ID: sourceID, Type: "synthetic", Streams: []string{"snap"}},
		streams: map[string][]page{
			"snap": {
				{rows: []model.Row{leagueRow("SCRATCH-R1")}},
				{rows: []model.Row{leagueRow("SCRATCH-R2")}, done: true},
			},
		},
	}
	rows, err := runner.RunSource(ctx, repaired, ModeOnce)
	if err != nil {
		t.Fatalf("resume run: %v", err)
	}
	if rows != 1 {
		t.Errorf("resumed rows = %d, want 1 (page 0 must be skipped by the watermark)", rows)
	}
}

func TestRunnerSeedModeIgnoresWatermark(t *testing.T) {
	pool := runnerPool(t)
	water := watermark.New(pool)
	ctx := context.Background()
	sourceID := fmt.Sprintf("scripted-seed-%d", time.Now().UnixNano())

	if err := water.Save(ctx, sourceID, "snap", map[string]any{"page": float64(9)}); err != nil {
		t.Fatalf("pre-save watermark: %v", err)
	}

	source := &scriptedSource{
		def: extract.SourceDef{ID: sourceID, Type: "synthetic", Streams: []string{"snap"}},
		streams: map[string][]page{
			"snap": {
				{rows: []model.Row{leagueRow("SCRATCH-S")}, done: true},
			},
		},
	}
	runner := newRunner(t, pool)
	rows, err := runner.RunSource(ctx, source, ModeSeed)
	if err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if rows != 1 {
		t.Errorf("seeded rows = %d, want 1", rows)
	}
	saved, err := water.Get(ctx, sourceID, "snap")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if saved["page"] != float64(1) {
		t.Errorf("watermark after seed = %v, want page 1 (overwritten)", saved)
	}
}
