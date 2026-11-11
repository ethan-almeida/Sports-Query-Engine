package watermark

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Get(ctx context.Context, sourceID, stream string) (map[string]any, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT cursor FROM ingest.watermarks WHERE source_id = $1 AND stream = $2`,
		sourceID, stream)
	if err != nil {
		return nil, fmt.Errorf("watermark: get %s/%s: %w", sourceID, stream, err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("watermark: get %s/%s: %w", sourceID, stream, err)
		}
		return nil, nil
	}
	raw, err := scanCursor(rows)
	if err != nil {
		return nil, fmt.Errorf("watermark: get %s/%s: %w", sourceID, stream, err)
	}
	return raw, nil
}

func (s *Store) Save(ctx context.Context, sourceID, stream string, cursor map[string]any) error {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return fmt.Errorf("watermark: encode cursor for %s/%s: %w", sourceID, stream, err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO ingest.watermarks (source_id, stream, cursor)
		 VALUES ($1, $2, $3::jsonb)
		 ON CONFLICT (source_id, stream)
		 DO UPDATE SET cursor = EXCLUDED.cursor, updated_at = now()`,
		sourceID, stream, string(encoded))
	if err != nil {
		return fmt.Errorf("watermark: save %s/%s: %w", sourceID, stream, err)
	}
	return nil
}

func scanCursor(rows interface {
	Values() ([]any, error)
}) (map[string]any, error) {
	values, err := rows.Values()
	if err != nil {
		return nil, err
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("expected 1 column, got %d", len(values))
	}
	if values[0] == nil {
		return nil, nil
	}
	switch value := values[0].(type) {
	case map[string]any:
		return value, nil
	case []byte:
		return decodeCursor(value)
	case string:
		return decodeCursor([]byte(value))
	default:
		return nil, fmt.Errorf("cursor column type %T is not readable JSON", values[0])
	}
}

func decodeCursor(raw []byte) (map[string]any, error) {
	var cursor map[string]any
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return nil, fmt.Errorf("decode cursor: %w", err)
	}
	return cursor, nil
}
