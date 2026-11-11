package espn

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"sportsql/internal/source"
)

// mapStatus maps an ESPN status (state + type name) onto our stored statuses:
// scheduled | live | final | postponed | canceled. The second return reports
// whether the input was a known value (false means we fell back on scores).
func mapStatus(state, typeName string, hasScore bool) (string, bool) {
	normalizedState := strings.ToLower(strings.TrimSpace(state))
	normalizedName := strings.ToUpper(strings.TrimSpace(typeName))
	switch {
	case strings.Contains(normalizedName, "POSTPONED"), strings.Contains(normalizedName, "SUSPENDED"):
		return "postponed", true
	case strings.Contains(normalizedName, "CANCELED"), strings.Contains(normalizedName, "CANCELLED"),
		strings.Contains(normalizedName, "ABANDONED"):
		return "canceled", true
	case strings.Contains(normalizedName, "FORFEIT"):
		return "final", true
	}
	switch normalizedState {
	case "in":
		return "live", true
	case "post", "final":
		return "final", true
	case "pre", "scheduled":
		return "scheduled", true
	case "postponed":
		return "postponed", true
	case "canceled":
		return "canceled", true
	}
	if hasScore {
		return "final", false
	}
	return "scheduled", false
}

// parseStartsAt parses the several timestamp shapes ESPN emits. It reports
// ok=false (and no error) for an empty value so callers can skip it.
func parseStartsAt(raw string) (time.Time, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false, nil
	}
	for _, layout := range []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04Z07:00",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
	} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.UTC(), true, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("event date %q does not match any known ESPN layout", raw)
}

// parseScore parses a non-negative integer score; empty yields (nil, nil).
func parseScore(raw string) (*int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value < 0 {
		return nil, fmt.Errorf("score %q is not a non-negative integer", raw)
	}
	return &value, nil
}

// scoreboardDate renders a date as ESPN's YYYYMMDD query parameter.
func scoreboardDate(day time.Time) string {
	return day.UTC().Format("20060102")
}

// dayFloor returns the UTC calendar day of t.
func dayFloor(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

var (
	easternOnce sync.Once
	easternLoc  *time.Location
)

// Eastern returns the league's home timezone (US Eastern for the NBA). ESPN's
// scoreboard dates= parameter and its game-day notion are Eastern-calendar-
// day based, so game dates are stored in this zone. If the system tzdata is
// missing we fall back to fixed EST (no DST).
func Eastern() *time.Location {
	easternOnce.Do(func() {
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			slog.Warn("tzdata missing; falling back to fixed EST", "error", err)
			loc = time.FixedZone("EST", -5*3600)
		}
		easternLoc = loc
	})
	return easternLoc
}

// gameDate returns the game's calendar day in the league's home timezone, as a
// UTC-midnight time.Time suitable for the games.game_date date column.
func gameDate(startsAt time.Time) time.Time {
	et := startsAt.In(Eastern())
	return time.Date(et.Year(), et.Month(), et.Day(), 0, 0, 0, 0, time.UTC)
}

// seasonFromGameDate applies the October rule: a game in October or later
// belongs to the season starting that year, an earlier game to the season
// that started the previous year. The result is the 4-digit start year.
func seasonFromGameDate(d time.Time) string {
	d = d.UTC()
	year := d.Year()
	if d.Month() >= time.October {
		return strconv.Itoa(year)
	}
	return strconv.Itoa(year - 1)
}

// slug turns a display name into a stable lower-case hyphen-separated code.
func slug(raw string) string {
	var builder strings.Builder
	lastDash := true
	for _, symbol := range strings.TrimSpace(raw) {
		switch {
		case unicode.IsLetter(symbol) || unicode.IsDigit(symbol):
			builder.WriteRune(unicode.ToLower(symbol))
			lastDash = false
		case !lastDash:
			builder.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

// ---- box-score stat mapping ---------------------------------------------
//
// A bxscr group carries a keys[] array parallel to each athlete's stats[]
// array, e.g.
//   keys:  ["minutes","points","fieldGoalsMade-fieldGoalsAttempted",...]
//   stats: ["39","32","11-22",...]
// Made-against values are "made-att". DNP players have an empty stats[] array.

var (
	keyMinutes    = "minutes"
	keyPoints     = "points"
	keyFieldGoals = "fieldGoalsMade-fieldGoalsAttempted"
	keyThrees     = "threePointFieldGoalsMade-threePointFieldGoalsAttempted"
	keyFrees      = "freeThrowsMade-freeThrowsAttempted"
	keyRebounds   = "rebounds"
	keyAssists    = "assists"
	keyTurnovers  = "turnovers"
	keySteals     = "steals"
	keyBlocks     = "blocks"
)

// statIndex maps a group's keys[] to the position of each stat we store, so
// the values can be read positionally from an athlete's stats[].
type statIndex struct {
	minutes, points, rebounds, assists, steals, blocks, turnovers int
	fieldGoals, threes, frees                                     int // made-att pairs
	ok                                                            bool
}

func buildStatIndex(keys []string) statIndex {
	idx := statIndex{
		minutes: -1, points: -1, rebounds: -1, assists: -1, steals: -1, blocks: -1,
		turnovers: -1, fieldGoals: -1, threes: -1, frees: -1,
	}
	for i, key := range keys {
		switch key {
		case keyMinutes:
			idx.minutes = i
		case keyPoints:
			idx.points = i
		case keyFieldGoals:
			idx.fieldGoals = i
		case keyThrees:
			idx.threes = i
		case keyFrees:
			idx.frees = i
		case keyRebounds:
			idx.rebounds = i
		case keyAssists:
			idx.assists = i
		case keyTurnovers:
			idx.turnovers = i
		case keySteals:
			idx.steals = i
		case keyBlocks:
			idx.blocks = i
		}
	}
	idx.ok = idx.minutes >= 0 && idx.points >= 0 && idx.fieldGoals >= 0 &&
		idx.threes >= 0 && idx.frees >= 0 && idx.rebounds >= 0
	return idx
}

// isDNP reports whether an athlete line is a did-not-play: no stats at all, or
// zero/blank minutes. Such rows store minutes as NULL so averages are safe.
func isDNP(idx statIndex, values []string) bool {
	if len(values) == 0 {
		return true
	}
	if idx.minutes >= 0 && idx.minutes < len(values) {
		m := strings.TrimSpace(values[idx.minutes])
		if m == "" || m == "0" {
			return true
		}
	}
	return false
}

func atIndex(values []string, i int) string {
	if i < 0 || i >= len(values) {
		return ""
	}
	return values[i]
}

func parseCount(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func parseMadeAtt(raw string) (made, att int) {
	parts := strings.Split(strings.TrimSpace(raw), "-")
	if len(parts) != 2 {
		return 0, 0
	}
	return parseCount(parts[0]), parseCount(parts[1])
}

// mapPlayerLine converts one bxscr athlete (keys + parallel stats) into a clean
// source.PlayerLine for the given team. minutes is nil for DNP.
func mapPlayerLine(gameID, teamCode, name, position string, idx statIndex, values []string) source.PlayerLine {
	line := source.PlayerLine{
		GameID:  gameID,
		Player:  source.Player{Code: slug(name), FullName: strings.TrimSpace(name), TeamCode: teamCode, Position: position},
		Minutes: nil,
	}
	if isDNP(idx, values) {
		return line
	}
	if m := parseCount(atIndex(values, idx.minutes)); m >= 0 {
		line.Minutes = &m
	}
	line.Points = parseCount(atIndex(values, idx.points))
	line.Rebounds = parseCount(atIndex(values, idx.rebounds))
	line.Assists = parseCount(atIndex(values, idx.assists))
	line.Steals = parseCount(atIndex(values, idx.steals))
	line.Blocks = parseCount(atIndex(values, idx.blocks))
	line.Turnovers = parseCount(atIndex(values, idx.turnovers))
	line.FgMade, line.FgAtt = parseMadeAtt(atIndex(values, idx.fieldGoals))
	line.ThreeMade, line.ThreeAtt = parseMadeAtt(atIndex(values, idx.threes))
	line.FtMade, line.FtAtt = parseMadeAtt(atIndex(values, idx.frees))
	return line
}
