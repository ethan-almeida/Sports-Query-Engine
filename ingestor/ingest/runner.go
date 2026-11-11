package ingest

import (
	"context"
	"fmt"
	"log/slog"

	"sports-analytics/ingestor/extract"
	"sports-analytics/ingestor/store"
	"sports-analytics/ingestor/watermark"
)

type Mode string

const (
	ModeOnce  Mode = "once"
	ModeSeed  Mode = "seed"
	ModeServe Mode = "serve"
)

const maxPagesPerStream = 1000

type Runner struct {
	logger   *slog.Logger
	store    *store.Store
	water    *watermark.Store
	retry    extract.RetryOptions
	maxPages int
}

func New(logger *slog.Logger, persistence *store.Store, water *watermark.Store, retry extract.RetryOptions) *Runner {
	if retry.MaxAttempts <= 0 {
		panic("ingest: retry options must have MaxAttempts > 0")
	}
	return &Runner{logger: logger, store: persistence, water: water, retry: retry, maxPages: maxPagesPerStream}
}

func (r *Runner) RunSource(ctx context.Context, source extract.Source, mode Mode) (int, error) {
	def := source.Def()
	total := 0
	for _, stream := range def.Streams {
		rows, err := r.runStream(ctx, source, stream, mode)
		if err != nil {
			return total, fmt.Errorf("ingest: source %s stream %s: %w", def.ID, stream, err)
		}
		total += rows
	}
	return total, nil
}

func (r *Runner) runStream(ctx context.Context, source extract.Source, stream string, mode Mode) (int, error) {
	def := source.Def()
	cursor := map[string]any(nil)
	if mode == ModeOnce {
		var err error
		cursor, err = r.water.Get(ctx, def.ID, stream)
		if err != nil {
			return 0, fmt.Errorf("read watermark: %w", err)
		}
		if cursor == nil {
			r.logger.Info("no stored watermark, starting fresh", "source", def.ID, "stream", stream)
		} else {
			r.logger.Info("resuming from watermark", "source", def.ID, "stream", stream, "cursor", cursor)
		}
	}

	rows := 0
	for page := 0; page < r.maxPages; page++ {
		var batch extract.Batch
		err := extract.Retry(ctx, r.logger, r.retry, fmt.Sprintf("fetch %s/%s", def.ID, stream), func(fetchCtx context.Context) error {
			fetched, fetchErr := source.Fetch(fetchCtx, stream, cursor)
			if fetchErr != nil {
				return fetchErr
			}
			batch = fetched
			return nil
		})
		if err != nil {
			return rows, fmt.Errorf("after page %d: %w", page, err)
		}
		if len(batch.Rows) > 0 {
			upserted, err := r.upsertBatch(ctx, def.ID, batch)
			if err != nil {
				return rows, err
			}
			rows += upserted
		}
		if err := r.water.Save(ctx, def.ID, stream, batch.Cursor); err != nil {
			return rows, err
		}
		r.logger.Debug("page ingested", "source", def.ID, "stream", stream,
			"page", page, "rows", len(batch.Rows), "cursor", batch.Cursor, "done", batch.Done)
		if batch.Done {
			r.logger.Info("stream complete", "source", def.ID, "stream", stream, "rows", rows)
			return rows, nil
		}
		cursor = batch.Cursor
	}
	return rows, fmt.Errorf("stream %s/%s exceeded %d pages without a Done batch", def.ID, stream, r.maxPages)
}

func (r *Runner) upsertBatch(ctx context.Context, sourceID string, batch extract.Batch) (int, error) {
	exec := store.Exec{Source: sourceID, Rows: batch.Rows}
	var upserted int
	err := extract.Retry(ctx, r.logger, r.retry, fmt.Sprintf("upsert %s/%s", sourceID, batch.Stream), func(upsertCtx context.Context) error {
		count, err := r.store.Upsert(upsertCtx, exec)
		if err != nil {
			return err
		}
		upserted = count
		return nil
	})
	if err != nil {
		return upserted, fmt.Errorf("ingest: upsert: %w", err)
	}
	return upserted, nil
}
