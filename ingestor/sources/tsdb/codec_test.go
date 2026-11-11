package tsdb

import (
	"testing"
	"time"
)

func TestMapStatusKnownValues(t *testing.T) {
	cases := []struct {
		status    string
		postponed string
		want      string
	}{
		{"FT", "no", "final"},
		{"Match Finished", "no", "final"},
		{"AET", "no", "final"},
		{"After Over Time", "no", "final"},
		{"After Pen", "no", "final"},
		{"1H", "no", "live"},
		{"HT", "no", "live"},
		{"LIVE", "no", "live"},
		{"Not Started", "no", "scheduled"},
		{"NS", "no", "scheduled"},
		{"TBD", "no", "scheduled"},
		{"Postponed", "no", "postponed"},
		{"Cancelled", "no", "canceled"},
		{"Abandoned", "no", "canceled"},
		{"Suspended", "no", "postponed"},
		{"ft", "no", "final"},
		{" match finished  ", "no", "final"},
	}
	for _, testCase := range cases {
		got, mapped := mapStatus(testCase.status, testCase.postponed, false)
		if got != testCase.want || !mapped {
			t.Errorf("mapStatus(%q,%q) = (%q,%v), want (%q,true)", testCase.status, testCase.postponed, got, mapped, testCase.want)
		}
	}
}

func TestMapStatusPostponedFlagWins(t *testing.T) {
	got, mapped := mapStatus("FT", "yes", true)
	if got != "postponed" || !mapped {
		t.Errorf("got (%q,%v), want (postponed,true)", got, mapped)
	}
}

func TestMapStatusUnknownFallsBackByScores(t *testing.T) {
	if got, mapped := mapStatus("SPECIAL RULE", "no", true); got != "final" || mapped {
		t.Errorf("scored fallback = (%q,%v), want (final,false)", got, mapped)
	}
	if got, mapped := mapStatus("SPECIAL RULE", "no", false); got != "scheduled" || mapped {
		t.Errorf("scoreless fallback = (%q,%v), want (scheduled,false)", got, mapped)
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
	if _, err := parseScore("three"); err == nil {
		t.Errorf("non-integer score must fail")
	}
	if _, err := parseScore("3-2"); err == nil {
		t.Errorf("composite score must fail")
	}
}

func TestParseStartsAtPrefersTimestamp(t *testing.T) {
	got, ok, err := parseStartsAt("2023-08-11T19:00:00", "1999-01-01", "00:00:00")
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	want := time.Date(2023, time.August, 11, 19, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseStartsAtKeepsOffset(t *testing.T) {
	got, ok, err := parseStartsAt("2023-08-14T12:00:00+02:00", "", "")
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	want := time.Date(2023, time.August, 14, 10, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseStartsAtDateAndTime(t *testing.T) {
	got, ok, err := parseStartsAt("", "2023-08-12", "11:30:00")
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	want := time.Date(2023, time.August, 12, 11, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseStartsAtDateOnly(t *testing.T) {
	got, ok, err := parseStartsAt("", "2023-08-12", "")
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	want := time.Date(2023, time.August, 12, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseStartsAtNone(t *testing.T) {
	if _, ok, err := parseStartsAt("", "", ""); ok || err != nil {
		t.Errorf("ok=%v err=%v, want (false,nil)", ok, err)
	}
}

func TestParseStartsAtRejectsGarbage(t *testing.T) {
	if _, _, err := parseStartsAt("yesterday", "", ""); err == nil {
		t.Errorf("garbage timestamp must fail")
	}
	if _, _, err := parseStartsAt("", "12/08/2023", ""); err == nil {
		t.Errorf("unparsed date layout must fail")
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Burnley":                  "burnley",
		"Brighton and Hove Albion": "brighton-and-hove-albion",
		"Wolverhampton  Wanderers": "wolverhampton-wanderers",
		"  Trimmed  Spaces  ":      "trimmed-spaces",
		"M. Kovačić":               "m-kovačić",
		"Team 42":                  "team-42",
	}
	for raw, want := range cases {
		if got := slug(raw); got != want {
			t.Errorf("slug(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestTeamCodePrefersShort(t *testing.T) {
	if got := teamCode("ARS", "Arsenal", "133604"); got != "ARS" {
		t.Errorf("got %q, want ARS", got)
	}
	if got := teamCode("", "Burnley", "123"); got != "burnley" {
		t.Errorf("got %q, want burnley", got)
	}
	if got := teamCode("", "", "42"); got != "tsdb-42" {
		t.Errorf("got %q, want tsdb-42", got)
	}
}

func TestIsCoach(t *testing.T) {
	for _, position := range []string{"Assistant Coach", "Head coach", "Manager", "Fitness Coach", "Technical Director", "Scout"} {
		if !isCoach(position) {
			t.Errorf("%q must be filtered as staff", position)
		}
	}
	if isCoach("Centre-Back") {
		t.Errorf("player positions must pass")
	}
}

func TestSportNormalizesSourceVariants(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"Soccer", "soccer"},
		{" soccer ", "soccer"},
		{"SOCCER", "soccer"},
		{"Esports", "esports"},
		{"Basketball", "basketball"},
		{"", ""},
	}
	for _, testCase := range cases {
		if got := sport(testCase.raw); got != testCase.want {
			t.Errorf("sport(%q) = %q, want %q", testCase.raw, got, testCase.want)
		}
	}
}
