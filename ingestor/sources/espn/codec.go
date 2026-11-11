package espn

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func mapStatus(state, typeName string, hasScore bool) (string, bool) {
	normalizedState := strings.ToLower(strings.TrimSpace(state))
	normalizedName := strings.ToUpper(strings.TrimSpace(typeName))
	switch {
	case strings.Contains(normalizedName, "POSTPONED"):
		return "postponed", true
	case strings.Contains(normalizedName, "SUSPENDED"):
		return "postponed", true
	case strings.Contains(normalizedName, "CANCELED"), strings.Contains(normalizedName, "CANCELLED"):
		return "canceled", true
	case strings.Contains(normalizedName, "ABANDONED"):
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

func parseStartsAt(raw string) (time.Time, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05Z07:00", "2006-01-02T15:04Z07:00", "2006-01-02 15:04:05Z07:00", "2006-01-02T15:04:05", "2006-01-02T15:04"} {
		parsed, err := time.Parse(layout, trimmed)
		if err == nil {
			return parsed.UTC(), true, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("event date %q does not match any known ESPN layout", raw)
}

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

func scoreboardDate(day time.Time) string {
	return day.UTC().Format("20060102")
}

func dayFloor(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func parseDateCursor(raw any) (time.Time, error) {
	switch value := raw.(type) {
	case string:
		day, err := time.ParseInLocation("20060102", strings.TrimSpace(value), time.UTC)
		if err != nil {
			return time.Time{}, fmt.Errorf("cursor date %q is not YYYYMMDD", value)
		}
		return day, nil
	default:
		return time.Time{}, fmt.Errorf("cursor date %T is not a string", raw)
	}
}

func parseSeasonYear(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	year, err := strconv.Atoi(trimmed)
	if err != nil || year < 1900 || year > 2200 {
		return 0, fmt.Errorf("param season %q is not a four-digit year", raw)
	}
	return year, nil
}

func parseSeasonBoundary(raw string, field string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	day, err := time.Parse("2006-01-02", strings.SplitN(trimmed, "T", 2)[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("core season %s %q is not a recognizable date", field, raw)
	}
	return day.UTC(), nil
}

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

func teamCode(abbreviation, name, id string) string {
	if code := strings.TrimSpace(abbreviation); code != "" {
		return code
	}
	if code := slug(name); code != "" {
		return code
	}
	return "espn-" + id
}

func positionName(position apiPosition) string {
	if name := strings.TrimSpace(position.DisplayName); name != "" {
		return name
	}
	return strings.TrimSpace(position.Abbreviation)
}
