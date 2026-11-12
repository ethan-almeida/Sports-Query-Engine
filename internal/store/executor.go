package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Executor runs LLM-generated SQL as the read-only role inside a READ ONLY
// transaction with a 5s statement timeout.
type Executor struct {
	pool *pgxpool.Pool
}

// NewExecutor builds an Executor over a connected pool.
func NewExecutor(pool *pgxpool.Pool) *Executor {
	return &Executor{pool: pool}
}

// QueryResult is one executed query's output.
type QueryResult struct {
	Columns []string
	Rows    [][]any
}

// Query runs the SQL and returns columns and rows.
func (e *Executor) Query(ctx context.Context, sql string) (QueryResult, error) {
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return QueryResult{}, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return QueryResult{}, fmt.Errorf("begin read-only transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '5s'"); err != nil {
		return QueryResult{}, fmt.Errorf("set statement timeout: %w", err)
	}

	rows, err := tx.Query(ctx, sql)
	if err != nil {
		return QueryResult{}, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	var columns []string
	for _, f := range fields {
		columns = append(columns, f.Name)
	}

	var out [][]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return QueryResult{}, fmt.Errorf("read row values: %w", err)
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, fmt.Errorf("iterate rows: %w", err)
	}
	return QueryResult{Columns: columns, Rows: out}, nil
}

// CurrentSeason returns the newest season present in the games table, or "" if
// the database has no games yet.
func (e *Executor) CurrentSeason(ctx context.Context) (string, error) {
	var season string
	err := e.pool.QueryRow(ctx, "SELECT MAX(season) FROM games").Scan(&season)
	if err != nil {
		return "", fmt.Errorf("current season: %w", err)
	}
	return season, nil
}

// DataAsOf returns the stored data_as_of value, or "" when unset.
func (e *Executor) DataAsOf(ctx context.Context) (string, error) {
	var value string
	err := e.pool.QueryRow(ctx, "SELECT value FROM meta WHERE key = 'data_as_of'").Scan(&value)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("data_as_of: %w", err)
	}
	return value, nil
}
