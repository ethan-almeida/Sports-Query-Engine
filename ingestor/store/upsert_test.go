package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"sports-analytics/ingestor/model"
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

func league(code string) model.Row {
	return model.Row{Kind: model.KindLeague, League: &model.League{Code: code, Name: "Store League " + code, Sport: "synthetic"}}
}

func team(leagueCode, code string) model.Row {
	return model.Row{Kind: model.KindTeam, Team: &model.Team{LeagueCode: leagueCode, Code: code, Name: "Store Team " + code, Abbreviation: code}}
}

func TestUpsertIsIdempotentAcrossRuns(t *testing.T) {
	pool := testPool(t)
	persistence := New(pool)
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	leagueCode := fmt.Sprintf("SID-%d", suffix)

	rows := []model.Row{league(leagueCode), team(leagueCode, "S-T1"), team(leagueCode, "S-T2")}
	first, err := persistence.Upsert(ctx, Exec{Source: "store-test", Rows: rows})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if first != 3 {
		t.Errorf("first upsert count = %d, want 3", first)
	}
	second, err := persistence.Upsert(ctx, Exec{Source: "store-test", Rows: rows})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if second != 3 {
		t.Errorf("second upsert count = %d, want 3", second)
	}

	var leagues, teams int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM sports.leagues WHERE code = $1`, leagueCode).Scan(&leagues)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM sports.teams WHERE league_code = $1`, leagueCode).Scan(&teams)
	if leagues != 1 {
		t.Errorf("league rows = %d, want 1 (idempotent upsert)", leagues)
	}
	if teams != 2 {
		t.Errorf("team rows = %d, want 2 (idempotent upsert)", teams)
	}

	var name string
	_ = pool.QueryRow(ctx, `SELECT name FROM sports.leagues WHERE code = $1`, leagueCode).Scan(&name)
	if name != "Store League "+leagueCode {
		t.Errorf("league name = %q", name)
	}
}

func TestUpsertRejectsEmptyAndIncompleteRows(t *testing.T) {
	pool := testPool(t)
	persistence := New(pool)
	ctx := context.Background()

	if _, err := persistence.Upsert(ctx, Exec{Source: "store-test", Rows: []model.Row{{Kind: model.KindLeague}}}); err == nil {
		t.Errorf("incomplete league must be rejected")
	}
	if _, err := persistence.Upsert(ctx, Exec{Source: "store-test", Rows: []model.Row{}}); err != nil {
		t.Errorf("empty row set must be accepted as a no-op: %v", err)
	}
}
