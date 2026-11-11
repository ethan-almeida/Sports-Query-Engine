package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"sports-analytics/ingestor/model"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type Exec struct {
	Source string
	Rows   []model.Row
}

func (s *Store) Upsert(ctx context.Context, exec Exec) (int, error) {
	if err := validate(exec.Rows); err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: begin: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	for _, pass := range passes(exec) {
		if len(pass.rows) == 0 {
			continue
		}
		batch := &pgx.Batch{}
		for _, row := range pass.rows {
			batch.Queue(pass.sql, pass.args(row, exec.Source)...)
		}
		results := tx.SendBatch(ctx, batch)
		for range pass.rows {
			if _, err := results.Exec(); err != nil {
				_ = results.Close()
				return 0, fmt.Errorf("store: upsert %s: %w", pass.kind, err)
			}
		}
		if err := results.Close(); err != nil {
			return 0, fmt.Errorf("store: flush %s: %w", pass.kind, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("store: commit: %w", err)
	}
	return len(exec.Rows), nil
}

func validate(rows []model.Row) error {
	for index, row := range rows {
		switch row.Kind {
		case model.KindLeague:
			if row.League == nil || row.League.Code == "" {
				return fmt.Errorf("store: row %d is an incomplete league", index)
			}
		case model.KindTeam:
			if row.Team == nil || row.Team.Code == "" || row.Team.LeagueCode == "" {
				return fmt.Errorf("store: row %d is an incomplete team", index)
			}
		case model.KindPlayer:
			if row.Player == nil || row.Player.Code == "" || row.Player.LeagueCode == "" {
				return fmt.Errorf("store: row %d is an incomplete player", index)
			}
		case model.KindGame:
			if row.Game == nil || row.Game.GameCode == "" || row.Game.LeagueCode == "" {
				return fmt.Errorf("store: row %d is an incomplete game", index)
			}
		case model.KindStat:
			if row.Stat == nil || row.Stat.StatName == "" || row.Stat.GameCode == "" || row.Stat.PlayerCode == "" || row.Stat.LeagueCode == "" {
				return fmt.Errorf("store: row %d is an incomplete stat", index)
			}
		default:
			return fmt.Errorf("store: row %d has unknown kind %d", index, row.Kind)
		}
	}
	return nil
}

func passes(exec Exec) []upsertPass {
	byKind := map[model.Kind][]model.Row{}
	for _, row := range exec.Rows {
		byKind[row.Kind] = append(byKind[row.Kind], row)
	}
	return []upsertPass{
		leaguePass(byKind),
		teamPass(byKind),
		playerPass(byKind),
		gamePass(byKind),
		statPass(byKind),
	}
}

type upsertPass struct {
	kind string
	sql  string
	rows []model.Row
	args func(model.Row, string) []any
}

func rowsFor(byKind map[model.Kind][]model.Row, kind model.Kind) []model.Row {
	return byKind[kind]
}

func leaguePass(byKind map[model.Kind][]model.Row) upsertPass {
	return upsertPass{
		kind: "leagues",
		sql: `INSERT INTO sports.leagues (code, name, sport)
		      VALUES ($1, $2, $3)
		      ON CONFLICT (code) DO UPDATE
		      SET name = EXCLUDED.name, sport = EXCLUDED.sport, updated_at = now()`,
		rows: rowsFor(byKind, model.KindLeague),
		args: func(row model.Row, source string) []any {
			league := row.League
			return []any{league.Code, league.Name, league.Sport}
		},
	}
}

func teamPass(byKind map[model.Kind][]model.Row) upsertPass {
	return upsertPass{
		kind: "teams",
		sql: `INSERT INTO sports.teams (league_code, code, name, abbreviation, conference, division, external_id, source)
		      VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7, $8)
		      ON CONFLICT (league_code, code) DO UPDATE
		      SET name = EXCLUDED.name, abbreviation = EXCLUDED.abbreviation,
		          conference = EXCLUDED.conference, division = EXCLUDED.division,
		          external_id = EXCLUDED.external_id, source = EXCLUDED.source, updated_at = now()`,
		rows: rowsFor(byKind, model.KindTeam),
		args: func(row model.Row, source string) []any {
			team := row.Team
			return []any{
				team.LeagueCode, team.Code, team.Name, team.Abbreviation,
				team.Conference, team.Division,
				source + ":" + team.Code, source,
			}
		},
	}
}

func playerPass(byKind map[model.Kind][]model.Row) upsertPass {
	return upsertPass{
		kind: "players",
		sql: `INSERT INTO sports.players (league_code, code, full_name, team_code, position, active, external_id, source)
		      VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8)
		      ON CONFLICT (league_code, code) DO UPDATE
		      SET full_name = EXCLUDED.full_name, team_code = EXCLUDED.team_code,
		          position = EXCLUDED.position, active = EXCLUDED.active,
		          external_id = EXCLUDED.external_id, source = EXCLUDED.source, updated_at = now()`,
		rows: rowsFor(byKind, model.KindPlayer),
		args: func(row model.Row, source string) []any {
			player := row.Player
			return []any{
				player.LeagueCode, player.Code, player.FullName, player.TeamCode,
				player.Position, player.Active,
				source + ":" + player.Code, source,
			}
		},
	}
}

func gamePass(byKind map[model.Kind][]model.Row) upsertPass {
	return upsertPass{
		kind: "games",
		sql: `INSERT INTO sports.games (league_code, game_code, season, starts_at, status, home_team_code, away_team_code, home_score, away_score, venue)
		      VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''))
		      ON CONFLICT (league_code, game_code) DO UPDATE
		      SET season = EXCLUDED.season, starts_at = EXCLUDED.starts_at, status = EXCLUDED.status,
		          home_team_code = EXCLUDED.home_team_code, away_team_code = EXCLUDED.away_team_code,
		          home_score = EXCLUDED.home_score, away_score = EXCLUDED.away_score,
		          venue = EXCLUDED.venue, updated_at = now()`,
		rows: rowsFor(byKind, model.KindGame),
		args: func(row model.Row, source string) []any {
			game := row.Game
			return []any{
				game.LeagueCode, game.GameCode, game.Season, game.StartsAt, game.Status,
				game.HomeTeamCode, game.AwayTeamCode, game.HomeScore, game.AwayScore, game.Venue,
			}
		},
	}
}

func statPass(byKind map[model.Kind][]model.Row) upsertPass {
	return upsertPass{
		kind: "player_stats",
		sql: `INSERT INTO sports.player_stats (league_code, game_code, player_code, stat_name, stat_value)
		      VALUES ($1, $2, $3, $4, $5)
		      ON CONFLICT (league_code, game_code, player_code, stat_name) DO UPDATE
		      SET stat_value = EXCLUDED.stat_value, updated_at = now()`,
		rows: rowsFor(byKind, model.KindStat),
		args: func(row model.Row, source string) []any {
			entry := row.Stat
			return []any{entry.LeagueCode, entry.GameCode, entry.PlayerCode, entry.StatName, entry.StatValue}
		},
	}
}
