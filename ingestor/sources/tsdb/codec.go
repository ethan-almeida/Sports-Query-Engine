package tsdb

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var statusMap = map[string]string{
	"ft":                 "final",
	"match finished":     "final",
	"finished":           "final",
	"finish":             "final",
	"aet":                "final",
	"after et":           "final",
	"after over time":    "final",
	"overtime finished":  "final",
	"pen":                "final",
	"after pen":          "final",
	"after penalties":    "final",
	"ft_pen":             "final",
	"1h":                 "live",
	"2h":                 "live",
	"ht":                 "live",
	"et":                 "live",
	"bt":                 "live",
	"p":                  "live",
	"live":               "live",
	"in play":            "live",
	"playing":            "live",
	"first half":         "live",
	"second half":        "live",
	"halftime":           "live",
	"overtime":           "live",
	"ns":                 "scheduled",
	"not started":        "scheduled",
	"tbd":                "scheduled",
	"time to be defined": "scheduled",
	"na":                 "scheduled",
	"postponed":          "postponed",
	"post.":              "postponed",
	"cancelled":          "canceled",
	"canceled":           "canceled",
	"abd":                "canceled",
	"abandoned":          "canceled",
	"susp":               "postponed",
	"suspended":          "postponed",
}

func mapStatus(rawStatus string, rawPostponed string, hasScore bool) (string, bool) {
	trimmedStatus := strings.TrimSpace(rawStatus)
	if strings.EqualFold(strings.TrimSpace(rawPostponed), "yes") || strings.EqualFold(trimmedStatus, "postponed") {
		return "postponed", true
	}
	normalized := strings.ToLower(trimmedStatus)
	status, ok := statusMap[normalized]
	if ok {
		return status, true
	}
	if hasScore {
		return "final", false
	}
	return "scheduled", false
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

func parseStartsAt(timestamp, date, timeOfDay string) (time.Time, bool, error) {
	timestamp = strings.TrimSpace(timestamp)
	if timestamp != "" {
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
			parsed, err := time.Parse(layout, timestamp)
			if err == nil {
				return parsed, true, nil
			}
		}
		return time.Time{}, false, fmt.Errorf("timestamp %q does not match any known layout", timestamp)
	}
	date = strings.TrimSpace(date)
	if date == "" {
		return time.Time{}, false, nil
	}
	day, err := time.Parse("2006-01-02", date)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("date %q is not YYYY-MM-DD", date)
	}
	hour, minute := 0, 0
	timeOfDay = strings.TrimSpace(timeOfDay)
	if timeOfDay != "" {
		clock, err := time.Parse("15:04:05", timeOfDay)
		if err != nil {
			if clock, err = time.Parse("15:04", timeOfDay); err != nil {
				return time.Time{}, false, fmt.Errorf("time %q is not HH:MM[:SS]", timeOfDay)
			}
		}
		hour, minute = clock.Hour(), clock.Minute()
	}
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, time.UTC), true, nil
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

func isCoach(position string) bool {
	normalized := strings.ToLower(position)
	return strings.Contains(normalized, "coach") ||
		strings.Contains(normalized, "manager") ||
		strings.Contains(normalized, "assistant") ||
		strings.Contains(normalized, "scout") ||
		strings.Contains(normalized, "director")
}

func sport(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
