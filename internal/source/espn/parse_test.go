package espn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sportsql/internal/source"
)

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseScoreboardFixture(t *testing.T) {
	body := loadFixture(t, "scoreboard.json")
	var payload scoreboardPayload
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	day := time.Date(2026, time.November, 10, 0, 0, 0, 0, time.UTC)
	// The fixture games start at 2026-11-10T00:00Z, which is 2026-11-09 19:00 in
	// US Eastern (EST in November). So the stored game date is 2026-11-09.
	wantGameDate := time.Date(2026, time.November, 9, 0, 0, 0, 0, time.UTC)
	_ = day
	games, skipped := parseScoreboard(payload)
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
	if len(games) < 2 {
		t.Fatalf("got %d games, want >= 2", len(games))
	}
	g := games[0]
	// 2026-11-09 19:00 ET == 2026-11-10T00:00Z -> game date 2026-11-09 (ET), season 2026.
	if g.GameID == "" {
		t.Error("game id empty")
	}
	if g.HomeTeam.Code == "" || g.AwayTeam.Code == "" {
		t.Errorf("team codes missing: home=%q away=%q", g.HomeTeam.Code, g.AwayTeam.Code)
	}
	if g.HomeTeam.Code == g.AwayTeam.Code {
		t.Errorf("home and away codes must differ, both %q", g.HomeTeam.Code)
	}
	if g.Season != "2026" {
		t.Errorf("season = %q, want 2026 (Nov 2026 game)", g.Season)
	}
	if g.Status != "final" {
		t.Errorf("status = %q, want final", g.Status)
	}
	if g.HomeScore == nil || g.AwayScore == nil {
		t.Errorf("final scores missing: %+v", g)
	}
	if g.Venue == "" {
		t.Error("venue empty")
	}
	if !g.Date.Equal(wantGameDate) {
		t.Errorf("game date = %v, want %v (ET calendar day of starts_at %v)", g.Date, wantGameDate, g.StartsAt)
	}
}

func TestParseScoreboardSkipsMalformedEvents(t *testing.T) {
	payload := scoreboardPayload{
		Events: []apiEvent{
			{}, // no id -> skipped
			{
				ID:   "123",
				Date: "not-a-date",
			},
		},
	}
	games, skipped := parseScoreboard(payload)
	if len(games) != 0 || skipped != 2 {
		t.Errorf("games=%d skipped=%d, want 0/2", len(games), skipped)
	}
}

func TestExtractJSONObjectBalances(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"a":1}`, `{"a":1}`},
		{`{"a":{"b":2}}`, `{"a":{"b":2}}`},
		{`{"s":"}{"}x}` + ``, `{"s":"}{"}`},
		{`{"s":"esc\\"}` + ``, `{"s":"esc\\"}`},
		{`{"a":1} trailing`, `{"a":1}`},
		{`no braces here`, ""},
	}
	for _, tc := range cases {
		if got := extractJSONObject(tc.in); got != tc.want {
			t.Errorf("extractJSONObject(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExtractBxscrFixture(t *testing.T) {
	page := loadFixture(t, "boxscore.html")
	bx, err := extractBxscr(page)
	if err != nil {
		t.Fatalf("extractBxscr: %v", err)
	}
	if len(bx) != 2 {
		t.Fatalf("got %d team boxes, want 2", len(bx))
	}
	// Determine which team is home.
	homeCount := 0
	teamCodes := map[string]bool{}
	for _, box := range bx {
		if box.Tm.Home {
			homeCount++
		}
		teamCodes[strings.ToLower(box.Tm.Abbrev)] = true
	}
	if homeCount != 1 {
		t.Errorf("homeCount = %d, want 1", homeCount)
	}
	if !teamCodes["lal"] || !teamCodes["atl"] {
		t.Errorf("team codes = %v, want lal and atl", teamCodes)
	}
}

func TestExtractBxscrRejectsChallengePage(t *testing.T) {
	if _, err := extractBxscr("<html><body>challenge</body></html>"); err == nil {
		t.Error("a page without the blob must fail")
	}
}

func TestParseBxscrFixture(t *testing.T) {
	page := loadFixture(t, "boxscore.html")
	bx, err := extractBxscr(page)
	if err != nil {
		t.Fatalf("extractBxscr: %v", err)
	}
	lines, err := parseBxscr("401909952", bx)
	if err != nil {
		t.Fatalf("parseBxscr: %v", err)
	}
	if len(lines) != 26 {
		t.Fatalf("got %d lines, want 26 (26 unique players in fixture)", len(lines))
	}

	var dnpCount int
	var donic source.PlayerLine
	hasDonic := false
	for _, line := range lines {
		if line.Minutes == nil {
			dnpCount++
		}
		if line.Player.Code == "luka-doncic" {
			donic = line
			hasDonic = true
		}
		if line.GameID != "401909952" {
			t.Errorf("line gameID = %q", line.GameID)
		}
	}
	if dnpCount != 6 {
		t.Errorf("dnpCount = %d, want 6 (empty-stat players in fixture)", dnpCount)
	}
	if !hasDonic {
		t.Fatal("luka-doncic not found")
	}
	d := donic
	if d.Minutes == nil || *d.Minutes != 39 {
		t.Errorf("doncic minutes = %v, want 39", d.Minutes)
	}
	if d.Points != 32 || d.ThreeAtt != 11 || d.ThreeMade != 3 || d.FgMade != 11 || d.FgAtt != 22 || d.Assists != 10 || d.Rebounds != 8 {
		t.Errorf("doncic stats wrong: %+v", d)
	}
	if d.Player.TeamCode != "LAL" {
		t.Errorf("doncic team = %q, want LAL", d.Player.TeamCode)
	}
	// DNP players keep identity and zero stats.
	var looney source.PlayerLine
	hasLooney := false
	for _, line := range lines {
		if line.Player.Code == "kevon-looney" {
			looney = line
			hasLooney = true
		}
	}
	if !hasLooney {
		t.Fatal("kevon-looney not found")
	}
	if looney.Minutes != nil {
		t.Errorf("looney minutes = %v, want nil", *looney.Minutes)
	}
	if looney.Points != 0 || looney.Rebounds != 0 {
		t.Errorf("looney stats must be zero: %+v", looney)
	}
}
