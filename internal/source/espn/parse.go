package espn

import (
	"fmt"
	"strings"

	"sportsql/internal/source"
)

// parseScoreboard converts a scoreboard payload into clean games, deriving the
// game date and season from each game's own start time. Malformed events are
// skipped and counted rather than failing the whole day.
func parseScoreboard(payload scoreboardPayload) (games []source.Game, skipped int) {
	for _, event := range payload.Events {
		game, ok := eventToGame(event)
		if !ok {
			skipped++
			continue
		}
		games = append(games, game)
	}
	return games, skipped
}

func eventToGame(event apiEvent) (source.Game, bool) {
	id := strings.TrimSpace(event.ID)
	if id == "" {
		return source.Game{}, false
	}
	startsAt, hasStart, err := parseStartsAt(event.Date)
	if err != nil || !hasStart {
		return source.Game{}, false
	}
	competition, ok := firstCompetition(event.Competitions)
	if !ok {
		return source.Game{}, false
	}
	home, away, found := homeAwayCompetitors(competition.Competitors)
	if !found {
		return source.Game{}, false
	}
	homeScore, err := parseScore(home.Score)
	if err != nil {
		return source.Game{}, false
	}
	awayScore, err := parseScore(away.Score)
	if err != nil {
		return source.Game{}, false
	}
	statusType := event.Status.Type
	status, _ := mapStatus(statusType.State, statusType.Name, homeScore != nil || awayScore != nil)
	venue := firstNonEmpty(competition.Venue.FullName, event.Venue.FullName)
	homeTeam := toTeam(home.Team)
	awayTeam := toTeam(away.Team)
	if homeTeam.Code == "" || awayTeam.Code == "" {
		return source.Game{}, false
	}
	// Season is derived from the game's own start date (October rule), never
	// from ESPN's season field (which uses the ending year).
	return source.Game{
		Date:      gameDate(startsAt),
		StartsAt:  startsAt,
		Season:    seasonFromGameDate(startsAt),
		Status:    status,
		HomeTeam:  homeTeam,
		AwayTeam:  awayTeam,
		HomeScore: homeScore,
		AwayScore: awayScore,
		Venue:     venue,
		GameID:    id,
	}, true
}

func toTeam(t apiCompetitorTeam) source.Team {
	code := strings.TrimSpace(t.Abbreviation)
	if code == "" {
		code = slug(firstNonEmpty(t.DisplayName, t.Name))
	}
	return source.Team{
		Code:         code,
		Name:         firstNonEmpty(t.DisplayName, t.Name),
		Abbreviation: strings.TrimSpace(t.Abbreviation),
	}
}

func firstCompetition(competitions []apiCompetition) (apiCompetition, bool) {
	if len(competitions) == 0 {
		return apiCompetition{}, false
	}
	return competitions[0], true
}

func homeAwayCompetitors(competitors []apiCompetitor) (home, away apiCompetitor, ok bool) {
	var haveHome, haveAway bool
	for _, competitor := range competitors {
		switch strings.ToLower(strings.TrimSpace(competitor.HomeAway)) {
		case "home":
			home = competitor
			haveHome = true
		case "away":
			away = competitor
			haveAway = true
		}
	}
	return home, away, haveHome && haveAway
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// ---- box score (summary API) ---------------------------------------------

// parseSummaryBoxscore converts a game summary into player lines. It reuses the
// scoreboard's positional stat keys, so a group whose keys[] are unreadable is
// treated as a totals/summary group and skipped. Did-not-play athletes keep a
// row with minutes nil so averages are safe.
func parseSummaryBoxscore(gameID string, payload summaryPayload) ([]source.PlayerLine, error) {
	boxes := payload.Boxscore.Players
	if len(boxes) == 0 {
		return nil, fmt.Errorf("summary has no boxscore players")
	}
	var lines []source.PlayerLine
	seenPlayers := map[string]bool{}
	for _, box := range boxes {
		teamCode := strings.TrimSpace(box.Team.Abbreviation)
		if teamCode == "" {
			return nil, fmt.Errorf("box for team without an abbreviation")
		}
		for _, group := range box.Statistics {
			idx := buildStatIndex(group.Keys)
			if !idx.ok {
				// A group without recognized keys (e.g. team totals) is skipped.
				if len(group.Athletes) == 0 {
					continue
				}
				return nil, fmt.Errorf("summary group lacks the expected stat keys")
			}
			for _, athlete := range group.Athletes {
				name := strings.TrimSpace(athlete.Athlete.DisplayName)
				if name == "" {
					continue
				}
				values := athlete.Stats
				if athlete.DidNotPlay {
					values = nil
				}
				line := mapPlayerLine(gameID, teamCode, name, athlete.Athlete.Position.Abbreviation, idx, values)
				if line.Player.Code == "" || seenPlayers[line.Player.Code] {
					continue
				}
				seenPlayers[line.Player.Code] = true
				lines = append(lines, line)
			}
		}
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("no player lines found in box score")
	}
	return lines, nil
}
