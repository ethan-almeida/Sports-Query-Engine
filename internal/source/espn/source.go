package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"sportsql/internal/source"
)

const (
	scoreboardPath = "/apis/site/v2/sports/basketball/nba/scoreboard"
	boxscorePath   = "/nba/boxscore/_/gameId/%s"
)

// Source implements source.Source against the public ESPN endpoints:
//
//   - GamesOn: JSON scoreboard API (site.api.espn.com)
//   - BoxScore: SSR page parse (www.espn.com/nba/boxscore/_/gameId/{id}),
//     because ESPN retired the JSON box-score endpoints.
type Source struct {
	client *client
}

var _ source.Source = (*Source)(nil)

// NewSource builds an ESPN source.
func NewSource(options Options) *Source {
	return &Source{client: newClient(options)}
}

// GamesOn returns every NBA game on the given date (UTC calendar day).
func (s *Source) GamesOn(ctx context.Context, date time.Time) ([]source.Game, error) {
	day := dayFloor(date)
	queries := url.Values{"dates": []string{scoreboardDate(day)}}
	body, err := s.client.get(ctx, s.client.siteBase+scoreboardPath+"?"+queries.Encode())
	if err != nil {
		return nil, fmt.Errorf("espn scoreboard %s: %w", scoreboardDate(day), err)
	}
	var payload scoreboardPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("espn scoreboard decode: %w", err)
	}
	games, skipped := parseScoreboard(payload)
	slog.Debug("espn scoreboard parsed", "date", scoreboardDate(day), "games", len(games), "skipped", skipped)
	return games, nil
}

// BoxScore returns every player line for the given game id, including
// did-not-play rows (minutes nil).
func (s *Source) BoxScore(ctx context.Context, gameID string) ([]source.PlayerLine, error) {
	gameID = strings.TrimSpace(gameID)
	if gameID == "" {
		return nil, fmt.Errorf("espn boxscore: empty game id")
	}
	path := s.client.webBase + fmt.Sprintf(boxscorePath, url.PathEscape(gameID))
	body, err := s.client.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("espn boxscore %s: %w", gameID, err)
	}
	bx, err := extractBxscr(string(body))
	if err != nil {
		return nil, fmt.Errorf("espn boxscore %s: %w", gameID, err)
	}
	lines, err := parseBxscr(gameID, bx)
	if err != nil {
		return nil, fmt.Errorf("espn boxscore %s: %w", gameID, err)
	}
	slog.Debug("espn boxscore parsed", "game", gameID, "lines", len(lines))
	return lines, nil
}
