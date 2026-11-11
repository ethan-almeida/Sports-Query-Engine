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
		{"post", "STATUS_FINAL", true, "final"},
		{"post", "STATUS_FULL_TIME", false, "final"},
		{"in", "STATUS_FIRST_HALF", false, "live"},
		{"in", "STATUS_HALFTIME", false, "live"},
		{"in", "STATUS_FINAL_PERIOD", false, "live"},
		{"pre", "STATUS_SCHEDULED", false, "scheduled"},
		{"postponed", "STATUS_POSTPONED", false, "postponed"},
		{"canceled", "STATUS_CANCELLED", false, "canceled"},
		{"post", "STATUS_CANCELED", true, "canceled"},
		{"", "STATUS_ABANDONED", true, "canceled"},
		{"", "STATUS_SUSPENDED", false, "postponed"},
	}
	for _, tc := range cases {
		got, mapped := mapStatus(tc.state, tc.name, tc.score)
		if got != tc.want || !mapped {
			t.Errorf("mapStatus(%q,%q,%v) = (%q,%v), want (%q,true)", tc.state, tc.name, tc.score, got, mapped, tc.want)
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

func TestParseStartsAtESPNShapes(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Time
	}{
		{"2024-05-19T15:00Z", time.Date(2024, time.May, 19, 15, 0, 0, 0, time.UTC)},
		{"2023-08-12T14:00:00Z", time.Date(2023, time.August, 12, 14, 0, 0, 0, time.UTC)},
		{"2023-08-15T19:00+01:00", time.Date(2023, time.August, 15, 18, 0, 0, 0, time.UTC)},
		{"2023-08-15T19:00:00+01:00", time.Date(2023, time.August, 15, 18, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		got, ok, err := parseStartsAt(tc.raw)
		if err != nil || !ok {
			t.Fatalf("parseStartsAt(%q) = (%v,%v,%v)", tc.raw, got, ok, err)
		}
		if !got.Equal(tc.want) {
			t.Errorf("parseStartsAt(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestParseStartsAtEmptyIsSkippable(t *testing.T) {
	if _, ok, err := parseStartsAt("  "); ok || err != nil {
		t.Errorf("empty date = (ok=%v,err=%v), want (false,nil)", ok, err)
	}
}

func TestParseStartsAtRejectsGarbage(t *testing.T) {
	if _, _, err := parseStartsAt("recently-ish"); err == nil {
		t.Error("garbage date must fail")
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
		t.Error("negative score must fail")
	}
	if _, err := parseScore("3-2"); err == nil {
		t.Error("composite score must fail")
	}
}

func TestScoreboardDate(t *testing.T) {
	day := time.Date(2023, time.August, 12, 14, 30, 0, 0, time.UTC)
	if got := scoreboardDate(day); got != "20230812" {
		t.Errorf("scoreboardDate = %q, want 20230812", got)
	}
}

func TestDayFloorIsUTC(t *testing.T) {
	loc := time.FixedZone("PST", -8*3600)
	tm := time.Date(2026, time.November, 9, 17, 0, 0, 0, loc) // 01:00 UTC Nov 10
	got := dayFloor(tm)
	if !got.Equal(time.Date(2026, time.November, 10, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("dayFloor = %v, want 2026-11-10 UTC", got)
	}
}

func TestSeasonFromGameDateOctoberRule(t *testing.T) {
	cases := []struct {
		date time.Time
		want string
	}{
		{time.Date(2026, time.October, 20, 1, 0, 0, 0, time.UTC), "2026"},
		{time.Date(2026, time.December, 31, 23, 0, 0, 0, time.UTC), "2026"},
		{time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC), "2026"},
		{time.Date(2027, time.June, 20, 0, 0, 0, 0, time.UTC), "2026"},
		{time.Date(2027, time.October, 1, 0, 0, 0, 0, time.UTC), "2027"},
		{time.Date(2022, time.February, 16, 0, 30, 0, 0, time.UTC), "2021"},
	}
	for _, tc := range cases {
		if got := seasonFromGameDate(tc.date); got != tc.want {
			t.Errorf("seasonFromGameDate(%s) = %q, want %q", tc.date.Format("2006-01-02"), got, tc.want)
		}
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Luka Doncic":              "luka-doncic",
		"  Giannis Antetokounmpo ": "giannis-antetokounmpo",
		"Jrue Holiday":             "jrue-holiday",
		"Klay Thompson":            "klay-thompson",
		"":                         "",
		"O'Neil":                   "o-neil",
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseMadeAtt(t *testing.T) {
	if made, att := parseMadeAtt("11-22"); made != 11 || att != 22 {
		t.Errorf("parseMadeAtt(11-22) = (%d,%d)", made, att)
	}
	if made, att := parseMadeAtt("0-0"); made != 0 || att != 0 {
		t.Errorf("parseMadeAtt(0-0) = (%d,%d)", made, att)
	}
	if made, att := parseMadeAtt(""); made != 0 || att != 0 {
		t.Errorf("parseMadeAtt(empty) = (%d,%d), want (0,0)", made, att)
	}
	if made, att := parseMadeAtt("garbage"); made != 0 || att != 0 {
		t.Errorf("parseMadeAtt(garbage) = (%d,%d), want (0,0)", made, att)
	}
}

func TestBuildStatIndex(t *testing.T) {
	keys := []string{
		"minutes", "points", "fieldGoalsMade-fieldGoalsAttempted",
		"threePointFieldGoalsMade-threePointFieldGoalsAttempted",
		"freeThrowsMade-freeThrowsAttempted", "rebounds", "assists",
		"turnovers", "steals", "blocks", "offensiveRebounds",
		"defensiveRebounds", "fouls", "plusMinus",
	}
	idx := buildStatIndex(keys)
	if !idx.ok {
		t.Fatal("index must be complete for the standard ESPN key list")
	}
	if idx.minutes != 0 || idx.points != 1 || idx.fieldGoals != 2 || idx.threes != 3 || idx.frees != 4 ||
		idx.rebounds != 5 || idx.assists != 6 || idx.turnovers != 7 || idx.steals != 8 || idx.blocks != 9 {
		t.Errorf("index positions wrong: %+v", idx)
	}
}

func TestBuildStatIndexMissingKeysNotOK(t *testing.T) {
	if (buildStatIndex(nil)).ok {
		t.Error("empty keys must not be ok")
	}
	if (buildStatIndex([]string{"minutes", "points"})).ok {
		t.Error("partial keys must not be ok")
	}
}

func TestIsDNP(t *testing.T) {
	idx := buildStatIndex([]string{"minutes", "points", "fieldGoalsMade-fieldGoalsAttempted",
		"threePointFieldGoalsMade-threePointFieldGoalsAttempted", "freeThrowsMade-freeThrowsAttempted", "rebounds"})
	if !isDNP(idx, nil) {
		t.Error("empty stats must be DNP")
	}
	if !isDNP(idx, []string{"", "0", "0-0", "0-0", "0-0", "0"}) {
		t.Error("blank/zero minutes must be DNP")
	}
	if isDNP(idx, []string{"28", "12", "5-10", "2-5", "0-0", "2"}) {
		t.Error("played minutes must not be DNP")
	}
}

func TestMapPlayerLinePlayed(t *testing.T) {
	keys := []string{
		"minutes", "points", "fieldGoalsMade-fieldGoalsAttempted",
		"threePointFieldGoalsMade-threePointFieldGoalsAttempted",
		"freeThrowsMade-freeThrowsAttempted", "rebounds", "assists",
		"turnovers", "steals", "blocks", "offensiveRebounds",
		"defensiveRebounds", "fouls", "plusMinus",
	}
	idx := buildStatIndex(keys)
	stats := []string{"39", "32", "11-22", "3-11", "7-7", "8", "10", "6", "2", "0", "0", "8", "2", "+4"}
	line := mapPlayerLine("g1", "DAL", "Luka Doncic", "G", idx, stats)
	if line.Player.Code != "luka-doncic" || line.Player.FullName != "Luka Doncic" || line.Player.TeamCode != "DAL" || line.Player.Position != "G" {
		t.Errorf("player identity wrong: %+v", line.Player)
	}
	if line.Minutes == nil || *line.Minutes != 39 {
		t.Errorf("minutes = %v, want 39", line.Minutes)
	}
	if line.Points != 32 || line.FgMade != 11 || line.FgAtt != 22 || line.ThreeMade != 3 || line.ThreeAtt != 11 ||
		line.FtMade != 7 || line.FtAtt != 7 || line.Rebounds != 8 || line.Assists != 10 ||
		line.Turnovers != 6 || line.Steals != 2 || line.Blocks != 0 {
		t.Errorf("stats wrong: %+v", line)
	}
	if line.GameID != "g1" {
		t.Errorf("gameID = %q, want g1", line.GameID)
	}
}

func TestMapPlayerLineDNP(t *testing.T) {
	keys := []string{"minutes", "points", "fieldGoalsMade-fieldGoalsAttempted",
		"threePointFieldGoalsMade-threePointFieldGoalsAttempted", "freeThrowsMade-freeThrowsAttempted", "rebounds"}
	idx := buildStatIndex(keys)
	line := mapPlayerLine("g1", "GSW", "Kevon Looney", "F", idx, nil)
	if line.Minutes != nil {
		t.Errorf("DNP minutes = %v, want nil", *line.Minutes)
	}
	if line.Points != 0 || line.Rebounds != 0 {
		t.Errorf("DNP stats must be zero: %+v", line)
	}
	if line.Player.Code != "kevon-looney" {
		t.Errorf("DNP player code = %q", line.Player.Code)
	}
}
