-- +goose Up

-- Season totals derived from game-level data. Season labels are 4-digit start
-- years (e.g. '2025' for the 2025-26 season). Percentages over a period are
-- SUM(made)/SUM(att), never the average of per-game percentages.
CREATE VIEW player_season_totals AS
SELECT
    p.code AS player_code,
    p.full_name,
    p.team_code,
    g.season,
    COUNT(*) FILTER (WHERE s.minutes IS NOT NULL) AS games_played,
    SUM(s.minutes) AS minutes,
    SUM(s.points) AS points,
    SUM(s.rebounds) AS rebounds,
    SUM(s.assists) AS assists,
    SUM(s.steals) AS steals,
    SUM(s.blocks) AS blocks,
    SUM(s.turnovers) AS turnovers,
    SUM(s.fg_made) AS fg_made,
    SUM(s.fg_att) AS fg_att,
    SUM(s.three_made) AS three_made,
    SUM(s.three_att) AS three_att,
    SUM(s.ft_made) AS ft_made,
    SUM(s.ft_att) AS ft_att,
    CASE WHEN SUM(s.fg_att) > 0
         THEN ROUND(SUM(s.fg_made)::numeric / SUM(s.fg_att), 4) END AS fg_pct,
    CASE WHEN SUM(s.three_att) > 0
         THEN ROUND(SUM(s.three_made)::numeric / SUM(s.three_att), 4) END AS three_pct,
    CASE WHEN SUM(s.ft_att) > 0
         THEN ROUND(SUM(s.ft_made)::numeric / SUM(s.ft_att), 4) END AS ft_pct,
    CASE WHEN COUNT(*) FILTER (WHERE s.minutes IS NOT NULL) > 0
         THEN ROUND(SUM(s.points)::numeric / COUNT(*) FILTER (WHERE s.minutes IS NOT NULL), 2) END AS ppg
FROM games g
JOIN player_game_stats s ON s.game_code = g.game_code
JOIN players p ON p.code = s.player_code
GROUP BY p.code, p.full_name, p.team_code, g.season;

-- +goose Down
DROP VIEW IF EXISTS player_season_totals;
