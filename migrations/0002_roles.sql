-- +goose Up
-- +goose NO TRANSACTION

-- app role configs

CREATE ROLE api_ro WITH LOGIN PASSWORD 'api_ro';
CREATE ROLE ingest_rw WITH LOGIN PASSWORD 'ingest_rw';

GRANT USAGE ON SCHEMA public TO api_ro, ingest_rw;
GRANT SELECT ON teams, players, games, player_game_stats, meta TO api_ro;
GRANT SELECT, INSERT, UPDATE ON teams, players, games, player_game_stats, meta TO ingest_rw;

-- +goose Down
REVOKE ALL ON teams, players, games, player_game_stats, meta FROM api_ro, ingest_rw;
REVOKE USAGE ON SCHEMA public FROM api_ro, ingest_rw;
DROP ROLE IF EXISTS api_ro;
DROP ROLE IF EXISTS ingest_rw;
