-- games.sql

-- Author: Ethan D'Almeida

-- This file creates the schema for a game in each sport. 

SET ROLE sports_ddl;

CREATE TABLE sports.games(
    league_code text NOT NULL,
    game_code text NOT NULL,
    season text NOT NULL,
    starts_at timestamptz NOT NULL, 
    status text NOT NULL,
    home_team_code text NOT NULL,
    away_team_code text NOT NULL,
    home_score integer,
    away_score integer,
    venue text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (league_code, game_code),
    FOREIGN KEY (league_code, home_team_code) REFERENCES sports.teams (league_code, code),
    FOREIGN KEY (league_code, away_team_code) REFERENCES sports.teams (league_code, code),
    CONSTRAINT games_status_check CHECK (status IN ('scheduled', 'live', 'final', 'postponed', 'canceled')),
    CONSTRAINT games_score_check CHECK ((home_score IS NULL OR home_score >= 0) AND (away_score IS NULL OR away_score >= 0))
);

CREATE INDEX idx_games_league_starts_at ON sports.games (league_code, starts_at);
GRANT SELECT ON sports.games TO sports_reader;
GRANT SELECT, INSERT, UPDATE ON sports.games TO sports_writer;