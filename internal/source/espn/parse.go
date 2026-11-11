package espn

import (
	"encoding/json"
	"fmt"
	"regexp"
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

// ---- box score (SSR page) ------------------------------------------------

// bxscrMarkerRe matches the prefix of the server-rendered blob that holds the
// box score. The page assigns it to window['__espnfitt__']. We brace-match the
// outer object from there and decode just the path we need, so the (large) rest
// of the page is never fully parsed.
var bxscrMarkerRe = regexp.MustCompile(`window\['__espnfitt__'\]\s*=\s*\{`)

// extractBxscr pulls the bxscr array out of an ESPN box-score page. The box
// score is server-rendered into the page as
//
//	window['__espnfitt__'] = { "page": { "content": { "gamepackage": { "bxscr": [...] } } } }
//
// We brace-match the outer object and decode just the path we need, so the
// (large) rest of the page is never fully parsed.
func extractBxscr(page string) ([]bxscrTeamBox, error) {
	loc := bxscrMarkerRe.FindStringIndex(page)
	if loc == nil {
		return nil, fmt.Errorf("no __espnfitt__ blob found (page layout changed or bot-challenge page served)")
	}
	// The match ends with the opening brace of the blob object.
	object := extractJSONObject(page[loc[1]-1:])
	if object == "" {
		return nil, fmt.Errorf("could not brace-match the __espnfitt__ object")
	}
	var payload fittPage
	if err := json.Unmarshal([]byte(object), &payload); err != nil {
		return nil, fmt.Errorf("decode __espnfitt__: %w", err)
	}
	if len(payload.Page.Content.GamePackage.Bxscr) == 0 {
		return nil, fmt.Errorf("bxscr array missing or empty")
	}
	return payload.Page.Content.GamePackage.Bxscr, nil
}

// extractJSONObject returns the balanced top-level JSON object starting at the
// beginning of s (s must begin with '{').
func extractJSONObject(s string) string {
	if len(s) == 0 || s[0] != '{' {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case inString:
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
		case ch == '"':
			inString = true
		case ch == '{':
			depth++
		case ch == '}':
			depth--
			if depth == 0 {
				return s[:i+1]
			}
		}
	}
	return ""
}

// parseBxscr converts the two bxscr team boxes into player lines. DNP players
// (empty stats array or zero minutes) keep a row with minutes nil.
func parseBxscr(gameID string, bx []bxscrTeamBox) ([]source.PlayerLine, error) {
	if len(bx) != 2 {
		return nil, fmt.Errorf("expected 2 team boxes, got %d", len(bx))
	}
	var lines []source.PlayerLine
	seenPlayers := map[string]bool{}
	for _, box := range bx {
		teamCode := strings.TrimSpace(box.Tm.Abbrev)
		if teamCode == "" {
			teamCode = slug(box.Tm.Name)
		}
		if teamCode == "" {
			return nil, fmt.Errorf("box for team without an abbreviation")
		}
		for _, group := range box.Stats {
			idx := buildStatIndex(group.Keys)
			if !idx.ok {
				// The totals group has no keys; other groups must be parseable.
				if len(group.Athlts) == 0 {
					continue
				}
				return nil, fmt.Errorf("box group %q lacks the expected stat keys", group.Type)
			}
			for _, athlete := range group.Athlts {
				line := mapPlayerLine(gameID, teamCode, athlete.Athlt.Name, athlete.Athlt.Pos, idx, athlete.Stats)
				if line.Player.Code == "" || line.Player.FullName == "" {
					continue
				}
				// ESPN box scores do not repeat a player; guard anyway.
				if seenPlayers[line.Player.Code] {
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
