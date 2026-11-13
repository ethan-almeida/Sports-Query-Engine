// Package prompt builds the two LLM prompts: SQL generation and answer writing.
package prompt

import (
	"fmt"
	"log/slog"
	"strings"

	"sportsql/internal/llm"
)

// Context carries the dynamic values injected into the prompts.
type Context struct {
	Today    string
	Season   string
	DataAsOf string
}

const systemSQL = `You write a single SQL query against a Postgres database of NBA games and box scores.

Schema:
teams(code text PK, name, abbreviation, conference, division)
players(code text PK, full_name, team_code -> teams.code, position)
games(game_code text PK, game_date date, season text, starts_at timestamptz,
      status, home_team_code, away_team_code, home_score, away_score, venue)
player_game_stats(game_code, player_code, minutes int NULL, points, rebounds,
      assists, steals, blocks, turnovers, fg_made, fg_att, three_made, three_att,
      ft_made, ft_att) -- PK (game_code, player_code), one row per player per game
player_season_totals(view: player_code, full_name, team_code, season, games_played,
      minutes, points, rebounds, assists, steals, blocks, turnovers, fg_made,
      fg_att, three_made, three_att, ft_made, ft_att, fg_pct, three_pct, ft_pct, ppg)
meta(key text PK, value)

Rules:
- Return ONLY the SQL. One SELECT statement, no semicolon, no other text.
- Only the tables and columns above exist. Do not invent columns.
- "this season" means season = the current season given below.
- minutes is NULL when a player did not play; for any average filter minutes > 0.
- Percentages over a period are SUM(made)/SUM(att), never the average of per-game values.
- For per-game averages use player_season_totals (it already excludes DNP from ppg)
  or divide by COUNT(*) FILTER (WHERE minutes IS NOT NULL).
- Team questions join teams on code; "Celtics" means name ILIKE '%Celtics%'.
- Keep results small: use ORDER BY and LIMIT where the question implies a ranking.
- Team record questions aggregate home AND away games into ONE row of wins and losses.
- "Game on <date>" lookups filter g.game_date = DATE '<date>' (the NBA game date), never starts_at::date.
- If the question names a team or player that might not exist in the data, guard the
  aggregate with HAVING COUNT(*) > 0 so a missing entity returns zero rows (not a 0 count).

Examples (question -> SQL):

Who attempted the most 3-pointers this season?
SELECT p.full_name, SUM(s.three_att) AS three_att
FROM player_game_stats s
JOIN games g ON g.game_code = s.game_code
JOIN players p ON p.code = s.player_code
WHERE g.season = '2025'
GROUP BY p.full_name
ORDER BY three_att DESC
LIMIT 1

What is the 3-point percentage leader this season?
SELECT full_name, ROUND(100.0 * three_made / three_att, 1) AS three_pct
FROM player_season_totals
WHERE season = '2025' AND three_att >= 100
ORDER BY three_pct DESC
LIMIT 1

How many points did Luka Doncic score last night?
SELECT s.points
FROM player_game_stats s
JOIN players p ON p.code = s.player_code
WHERE p.full_name ILIKE '%Doncic%' AND s.game_code = (
  SELECT game_code FROM games ORDER BY game_date DESC LIMIT 1
)

What is the Celtics' record this season?
SELECT COUNT(*) FILTER (WHERE won) AS wins, COUNT(*) FILTER (WHERE NOT won) AS losses
FROM (
  SELECT (home_score > away_score) AS won FROM games g JOIN teams t ON t.code = g.home_team_code
  WHERE t.name ILIKE '%Celtics%' AND g.season = '2025' AND g.status = 'final'
  UNION ALL
  SELECT (away_score > home_score) FROM games g JOIN teams t ON t.code = g.away_team_code
  WHERE t.name ILIKE '%Celtics%' AND g.season = '2025' AND g.status = 'final'
) AS all_games

Top 5 rebounders this season?
SELECT full_name, rebounds
FROM player_season_totals
WHERE season = '2025'
ORDER BY rebounds DESC
LIMIT 5

Which game did the Warriors play on 2026-03-15?
SELECT g.game_date, t1.name || ' @ ' || t2.name, g.away_score || '-' || g.home_score
FROM games g
JOIN teams t1 ON t1.code = g.home_team_code
JOIN teams t2 ON t2.code = g.away_team_code
WHERE g.game_date = DATE '2026-03-15' AND (t1.name ILIKE '%Warriors%' OR t2.name ILIKE '%Warriors%')

Who leads the league in steals this season?
SELECT full_name, steals
FROM player_season_totals
WHERE season = '2025'
ORDER BY steals DESC
LIMIT 1

How many games has Giannis Antetokounmpo played this season?
SELECT games_played
FROM player_season_totals
WHERE season = '2025' AND full_name ILIKE '%Antetokounmpo%'

How many points did the Grizzlies score this season?
SELECT SUM(s.points) AS points
FROM player_game_stats s
JOIN games g ON g.game_code = s.game_code
JOIN players p ON p.code = s.player_code
JOIN teams t ON t.code = p.team_code
WHERE t.name ILIKE '%Grizzlies%' AND g.season = '2025'
HAVING COUNT(*) > 0`

const systemAnswer = `You answer a question about NBA games using ONLY the query results below.
- The results are authoritative: report exactly what they show. Never use, invent, or second-guess any number that is not in the results, and never comment on whether the data is real, recent, or plausible.
- If the results do not contain what the question asks, say so plainly.
- Reply with one or two plain sentences. No markdown, no lists, no SQL.`

// SQLPrompt builds the messages for the SQL-generation call. When failedSQL and
// errMsg are non-empty it appends the earlier failure as a
// hint to write a corrected query.
func SQLPrompt(ctx Context, question, failedSQL, errMsg string) []llm.Message {
	user := fmt.Sprintf(
		"Today is %s. The current season is %s. Data goes up to %s.\nQuestion: %s",
		ctx.Today, ctx.Season, ctx.DataAsOf, strings.TrimSpace(question))
	if failedSQL != "" || errMsg != "" {
		user += fmt.Sprintf(
			"\n\nYour previous SQL (%s) was rejected: %s. Return only one corrected SELECT using the same schema; no explanation.",
			strings.TrimSpace(failedSQL), strings.TrimSpace(errMsg))
	}
	messages := []llm.Message{
		{Role: "system", Content: systemSQL},
		{Role: "user", Content: user},
	}
	size := 0
	for _, m := range messages {
		size += len(m.Content)
	}
	slog.Debug("sql prompt built", "chars", size, "approx_tokens", size/4)
	return messages
}

// AnswerPrompt builds the messages for the answer-writing call. rows is the
// rendered query output.
func AnswerPrompt(ctx Context, question, sql, rows string) []llm.Message {
	return []llm.Message{
		{Role: "system", Content: systemAnswer},
		{Role: "user", Content: fmt.Sprintf(
			"Question: %s\n\nSQL:\n%s\n\nResults:\n%s\n\nAnswer using only the numbers in the results:",
			strings.TrimSpace(question), strings.TrimSpace(sql), strings.TrimSpace(rows))},
	}
}
