// Package store owns Postgres access. The ingest writer (M2) upserts games,
// players, teams and player_game_stats as the ingest_rw role. A separate
// read-only executor for the API lands in M3.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"sportsql/internal/source"
)

// Store wraps a pgx pool used for ingest writes.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store over an existing pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

const (
	upsertTeam = `INSERT INTO teams (code, name, abbreviation)
		VALUES ($1, $2, $3)
		ON CONFLICT (code) DO UPDATE
		SET name = EXCLUDED.name,
		    abbreviation = EXCLUDED.abbreviation,
		    updated_at = now()`

	upsertPlayer = `INSERT INTO players (code, full_name, team_code, position)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (code) DO UPDATE
		SET full_name = EXCLUDED.full_name,
		    team_code = EXCLUDED.team_code,
		    position = EXCLUDED.position,
		    updated_at = now()`

	upsertGame = `INSERT INTO games (game_code, game_date, season, starts_at, status,
		                            home_team_code, away_team_code, home_score, away_score, venue)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (game_code) DO UPDATE
		SET game_date = EXCLUDED.game_date,
		    season = EXCLUDED.season,
		    starts_at = EXCLUDED.starts_at,
		    status = EXCLUDED.status,
		    home_team_code = EXCLUDED.home_team_code,
		    away_team_code = EXCLUDED.away_team_code,
		    home_score = EXCLUDED.home_score,
		    away_score = EXCLUDED.away_score,
		    venue = EXCLUDED.venue,
		    updated_at = now()`

	upsertStat = `INSERT INTO player_game_stats (game_code, player_code, minutes, points,
		                                      rebounds, assists, steals, blocks, turnovers,
		                                      fg_made, fg_att, three_made, three_att, ft_made, ft_att)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (game_code, player_code) DO UPDATE
		SET minutes = EXCLUDED.minutes,
		    points = EXCLUDED.points,
		    rebounds = EXCLUDED.rebounds,
		    assists = EXCLUDED.assists,
		    steals = EXCLUDED.steals,
		    blocks = EXCLUDED.blocks,
		    turnovers = EXCLUDED.turnovers,
		    fg_made = EXCLUDED.fg_made,
		    fg_att = EXCLUDED.fg_att,
		    three_made = EXCLUDED.three_made,
		    three_att = EXCLUDED.three_att,
		    ft_made = EXCLUDED.ft_made,
		    ft_att = EXCLUDED.ft_att,
		    updated_at = now()`

	setDataAsOf = `INSERT INTO meta (key, value) VALUES ('data_as_of', $1)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`
)

// GameStats bundles one game with its player lines for a single transaction.
type GameStats struct {
	Game  source.Game
	Lines []source.PlayerLine
}

// Save persists a day's games and their box scores idempotently, in one
// transaction, so a partial day never half-lands. Rows are deduped and
// inserted in FK-safe order: teams, players, games, then player_game_stats.
func (s *Store) Save(ctx context.Context, day []GameStats) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	teams := map[string]source.Team{}
	players := map[string]source.Player{}
	for _, gs := range day {
		teams[gs.Game.HomeTeam.Code] = gs.Game.HomeTeam
		teams[gs.Game.AwayTeam.Code] = gs.Game.AwayTeam
		for _, line := range gs.Lines {
			players[line.Player.Code] = line.Player
		}
	}

	batch := &pgx.Batch{}
	for _, team := range teams {
		batch.Queue(upsertTeam, team.Code, team.Name, nullable(team.Abbreviation))
	}
	for _, player := range players {
		batch.Queue(upsertPlayer, player.Code, player.FullName, nullable(player.TeamCode), nullable(player.Position))
	}
	for _, gs := range day {
		g := gs.Game
		batch.Queue(upsertGame, g.GameID, g.Date, g.Season, g.StartsAt, g.Status,
			g.HomeTeam.Code, g.AwayTeam.Code, g.HomeScore, g.AwayScore, nullable(g.Venue))
		for _, line := range gs.Lines {
			batch.Queue(upsertStat,
				line.GameID, line.Player.Code, line.Minutes,
				line.Points, line.Rebounds, line.Assists, line.Steals, line.Blocks, line.Turnovers,
				line.FgMade, line.FgAtt, line.ThreeMade, line.ThreeAtt, line.FtMade, line.FtAtt)
		}
	}

	if batch.Len() > 0 {
		results := tx.SendBatch(ctx, batch)
		for i := 0; i < batch.Len(); i++ {
			if _, err := results.Exec(); err != nil {
				_ = results.Close()
				return fmt.Errorf("store: upsert %d: %w", i, err)
			}
		}
		if err := results.Close(); err != nil {
			return fmt.Errorf("store: flush: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// SetDataAsOf records the latest ingested game date in meta.
func (s *Store) SetDataAsOf(ctx context.Context, value string) error {
	if _, err := s.pool.Exec(ctx, setDataAsOf, value); err != nil {
		return fmt.Errorf("store: set data_as_of: %w", err)
	}
	return nil
}

// MaxGameDate returns the latest game_date in the table, or ok=false if the
// table is empty (so callers can fall back to a default --from).
func (s *Store) MaxGameDate(ctx context.Context) (value string, ok bool, err error) {
	var max *string
	row := s.pool.QueryRow(ctx, `SELECT max(game_date)::text FROM games`)
	if err := row.Scan(&max); err != nil {
		return "", false, fmt.Errorf("store: max game_date: %w", err)
	}
	if max == nil {
		return "", false, nil
	}
	return *max, true, nil
}

// nullable converts an empty string to a NULL column value.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
