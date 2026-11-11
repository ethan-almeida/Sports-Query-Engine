-- player_stats.sql

-- Schema for player stats in different sports


SET ROLE sports_ddl;

CREATE TABLE sports.players (
    league_code text        NOT NULL,
    code        text        NOT NULL,     
    full_name   text        NOT NULL,
    team_code   text,                     
    position    text,
    active      boolean     NOT NULL DEFAULT true,
    external_id text,
    source      text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (league_code, code),
    FOREIGN KEY (league_code, team_code) REFERENCES sports.teams (league_code, code)
);

CREATE TABLE sports.player_stats (
    league_code text        NOT NULL,
    game_code   text        NOT NULL,
    player_code text        NOT NULL,
    stat_name   text        NOT NULL,     
    stat_value  numeric     NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (league_code, game_code, player_code, stat_name),
    FOREIGN KEY (league_code, game_code)  REFERENCES sports.games   (league_code, game_code),
    FOREIGN KEY (league_code, player_code) REFERENCES sports.players (league_code, code)
);

CREATE INDEX idx_player_stats_player ON sports.player_stats (league_code, player_code);
GRANT SELECT ON sports.players, sports.player_stats TO sports_reader;
GRANT SELECT, INSERT, UPDATE ON sports.players, sports.player_stats TO sports_writer;