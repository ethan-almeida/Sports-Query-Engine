package espn

import (
	"testing"
	"time"
)

func TestMapStatusKnownValues(t *testing.T) {
	cases := []struct {
		state string
		name  string
		score bool
		want  string
	}{
		{"post", "STATUS_FULL_TIME", true, "final"},
		{"post", "STATUS_FINAL", true, "final"},
		{"post", "STATUS_FULL_TIME", false, "final"},
		{"in", "STATUS_FIRST_HALF", false, "live"},
		{"in", "STATUS_HALFTIME", false, "live"},
		{"in", "STATUS_FINAL_PERIOD", false, "live"},
		{"pre", "STATUS_SCHEDULED", false, "scheduled"},
		{"pre", "STATUS_SCHEDULED", false, "scheduled"},
		{"postponed", "STATUS_POSTPONED", false, "postponed"},
		{"canceled", "STATUS_CANCELLED", false, "canceled"},
		{"post", "STATUS_CANCELED", true, "canceled"},
		{"", "STATUS_ABANDONED", true, "canceled"},
		{"", "STATUS_SUSPENDED", false, "postponed"},
	}
	for _, testCase := range cases {
		got, mapped := mapStatus(testCase.state, testCase.name, testCase.score)
		if got != testCase.want || !mapped {
			t.Errorf("mapStatus(%q,%q,%v) = (%q,%v), want (%q,true)", testCase.state, testCase.name, testCase.score, got, mapped, testCase.want)
		}
	}
}

func TestMapStatusForfeitCountsAsFinal(t *testing.T) {
	if got, mapped := mapStatus("post", "STATUS_FORFEIT", true); got != "final" || !mapped {
		t.Errorf("got (%q,%v), want (final,true)", got, mapped)
	}
}

func TestMapStatusUnknownFallsBackByScores(t *testing.T) {
	if got, mapped := mapStatus("zzz", "STATUS_WEIRD", true); got != "final" || mapped {
		t.Errorf("scored fallback = (%q,%v), want (final,false)", got, mapped)
	}
	if got, mapped := mapStatus("zzz", "STATUS_WEIRD", false); got != "scheduled" || mapped {
		t.Errorf("scoreless fallback = (%q,%v), want (scheduled,false)", got, mapped)
	}
}

func TestParseStartsESPNShapes(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Time
		ok   bool
	}{
		{"2024-05-19T15:00Z", time.Date(2024, time.May, 19, 15, 0, 0, 0, time.UTC), true},
		{"2023-08-12T14:00:00Z", time.Date(2023, time.August, 12, 14, 0, 0, 0, time.UTC), true},
		{"2023-08-15T19:00+01:00", time.Date(2023, time.August, 15, 18, 0, 0, 0, time.UTC), true},
		{"2023-08-15T19:00:00+01:00", time.Date(2023, time.August, 15, 18, 0, 0, 0, time.UTC), true},
	}
	for _, testCase := range cases {
		got, ok, err := parseStartsAt(testCase.raw)
		if err != nil || !ok {
			t.Fatalf("parseStartsAt(%q) = (%v,%v,%v)", testCase.raw, got, ok, err)
		}
		if !got.Equal(testCase.want) {
			t.Errorf("parseStartsAt(%q) = %v, want %v", testCase.raw, got, testCase.want)
		}
	}
}

func TestParseStartsAtEmptyIsSkippable(t *testing.T) {
	if _, ok, err := parseStartsAt("  "); ok || err != nil {
		t.Errorf("empty date = (%v,%v), want (false,nil)", ok, err)
	}
}

func TestParseStartsAtRejectsGarbage(t *testing.T) {
	if _, _, err := parseStartsAt("recently-ish"); err == nil {
		t.Errorf("garbage date must fail")
	}
}

func TestParseScore(t *testing.T) {
	if score, err := parseScore(""); score != nil || err != nil {
		t.Errorf("empty score = (%v,%v), want (nil,nil)", score, err)
	}
	three, err := parseScore(" 3 ")
	if err != nil || three == nil || *three != 3 {
		t.Errorf("parseScore = (%v,%v)", three, err)
	}
	if _, err := parseScore("-1"); err == nil {
		t.Errorf("negative score must fail")
	}
	if _, err := parseScore("3-2"); err == nil {
		t.Errorf("composite score must fail")
	}
}

func TestScoreboardDate(t *testing.T) {
	day := time.Date(2023, time.August, 12, 14, 30, 0, 0, time.UTC)
	if got := scoreboardDate(day); got != "20230812" {
		t.Errorf("scoreboardDate = %q, want 20230812", got)
	}
	parsed, err := parseDateCursor("20230812")
	if err != nil {
		t.Fatalf("parseDateCursor: %v", err)
	}
	if !parsed.Equal(time.Date(2023, time.August, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("parseDateCursor = %v", parsed)
	}
	if _, err := parseDateCursor(12.5); err == nil {
		t.Errorf("number dates must fail")
	}
	if _, err := parseDateCursor("2023-08-12"); err == nil {
		t.Errorf("dashed dates must fail")
	}
}

func TestParseSeasonYear(t *testing.T) {
	if year, err := parseSeasonYear(" 2023 "); err != nil || year != 2023 {
		t.Errorf("parseSeasonYear = (%d,%v)", year, err)
	}
	for _, raw := range []string{"2023-2024", "abc", "23", "3999"} {
		if _, err := parseSeasonYear(raw); err == nil {
			t.Errorf("season %q must fail", raw)
		}
	}
}

func TestParseSeasonBoundary(t *testing.T) {
	start, err := parseSeasonBoundary("2023-08-12T00:00Z", "start_date")
	if err != nil {
		t.Fatalf("parseSeasonBoundary: %v", err)
	}
	if !start.Equal(time.Date(2023, time.August, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("start = %v", start)
	}
	end, err := parseSeasonBoundary("2023-08-22", "end_date")
	if err != nil {
		t.Fatalf("parseSeasonBoundary: %v", err)
	}
	if !end.Equal(time.Date(2023, time.August, 22, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("end = %v", end)
	}
	if _, err := parseSeasonBoundary("not-a-date", "end_date"); err == nil {
		t.Errorf("garbage boundary must fail")
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Arsenal":             "arsenal",
		"AFC Bournemouth":     "afc-bournemouth",
		"  Trimmed  Spaces  ": "trimmed-spaces",
		"M. Kovačić":          "m-kovačić",
		"Team 42":             "team-42",
	}
	for raw, want := range cases {
		if got := slug(raw); got != want {
			t.Errorf("slug(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestTeamCodePrefersAbbreviation(t *testing.T) {
	if got := teamCode("ARS", "Arsenal", "359"); got != "ARS" {
		t.Errorf("got %q, want ARS", got)
	}
	if got := teamCode("", "AFC Bournemouth", "349"); got != "afc-bournemouth" {
		t.Errorf("got %q, want afc-bournemouth", got)
	}
	if got := teamCode("", "", "42"); got != "espn-42" {
		t.Errorf("got %q, want espn-42", got)
	}
}

func TestPositionNamePrefersDisplayName(t *testing.T) {
	got := positionName(apiPosition{Abbreviation: "G", DisplayName: "Goalkeeper"})
	if got != "Goalkeeper" {
		t.Errorf("got %q, want Goalkeeper", got)
	}
	if got := positionName(apiPosition{Abbreviation: "G"}); got != "G" {
		t.Errorf("got %q, want G", got)
	}
}
