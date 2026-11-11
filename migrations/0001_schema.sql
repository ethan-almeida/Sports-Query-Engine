-- +goose Up
-- +goose NO TRANSACTION

-- schema for NBA box scores only

CREATE TABLE IF NOT EXISTS teams (
    code         text PRIMARY KEY,
    name         text NOT NULL,
    abbreviation text,
    conference   text,
    division     text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS players (
    code       text PRIMARY KEY,
    full_name  text NOT NULL,
    team_code  text REFERENCES teams(code),
    position   text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS games (
    game_code      text PRIMARY KEY,
    game_date      date NOT NULL,
    season         text NOT NULL,
    starts_at      timestamptz NOT NULL,
    status         text NOT NULL,
    home_team_code text NOT NULL REFERENCES teams(code),
    away_team_code text NOT NULL REFERENCES teams(code),
    home_score     integer,
    away_score     integer,
    venue          text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT games_status_check CHECK (status IN ('scheduled', 'live', 'final', 'postponed', 'canceled')),
    CONSTRAINT games_score_check CHECK ((home_score IS NULL OR home_score >= 0) AND (away_score IS NULL OR away_score >= 0))
);

CREATE INDEX IF NOT EXISTS idx_games_game_date ON games (game_date);

-- One row per player per game. minutes is NULL for did-not-play (DNP) games
-- so DNP rows do not distort averages; prompt rules filter minutes > 0 for
-- any per-game average.
CREATE TABLE IF NOT EXISTS player_game_stats (
    game_code  text NOT NULL REFERENCES games(game_code),
    player_code text NOT NULL REFERENCES players(code),
    minutes    integer,
    points     integer NOT NULL DEFAULT 0,
    rebounds   integer NOT NULL DEFAULT 0,
    assists    integer NOT NULL DEFAULT 0,
    steals     integer NOT NULL DEFAULT 0,
    blocks     integer NOT NULL DEFAULT 0,
    turnovers  integer NOT NULL DEFAULT 0,
    fg_made    integer NOT NULL DEFAULT 0,
    fg_att     integer NOT NULL DEFAULT 0,
    three_made integer NOT NULL DEFAULT 0,
    three_att  integer NOT NULL DEFAULT 0,
    ft_made    integer NOT NULL DEFAULT 0,
    ft_att     integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (game_code, player_code),
    CONSTRAINT pgs_fg_check CHECK (fg_made <= fg_att),
    CONSTRAINT pgs_three_check CHECK (three_made <= three_att),
    CONSTRAINT pgs_ft_check CHECK (ft_made <= ft_att)
);

CREATE INDEX IF NOT EXISTS idx_pgs_player ON player_game_stats (player_code);

CREATE TABLE IF NOT EXISTS meta (
    key   text PRIMARY KEY,
    value text,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS meta;
DROP TABLE IF EXISTS player_game_stats;
DROP TABLE IF EXISTS games;
DROP TABLE IF EXISTS players;
DROP TABLE IF EXISTS teams;
