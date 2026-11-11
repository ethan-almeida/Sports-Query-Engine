package espn

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"sports-analytics/ingestor/extract"
	"sports-analytics/ingestor/model"
)

type waitRecorder struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (w *waitRecorder) wait(ctx context.Context, d time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.waits = append(w.waits, d)
	return nil
}

type fakeAPI struct {
	mu     sync.Mutex
	counts map[string]int
	router func(r *http.Request) string
}

const seasonWindowFixture = `{"year":2023,"startDate":"2023-08-12T00:00Z","endDate":"2023-08-22T23:00Z"}`

const teamsFixture = `{"sports":[{"leagues":[{"name":"English Premier League","teams":[
	{"team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal","slug":"eng.arsenal","isActive":true}},
	{"team":{"id":"349","abbreviation":"","displayName":"AFC Bournemouth","slug":"eng.bournemouth","isActive":true}}
]}]}]}`

const arsenalRosterFixture = `{"team":{"id":"359","athletes":[
	{"id":"1","fullName":"Bukayo Saka","displayName":"Bukayo Saka","position":{"abbreviation":"RW","displayName":"Right Winger"}},
	{"id":"2","fullName":"A Smith","displayName":"A Smith","position":{"abbreviation":"DM","displayName":"Defensive Midfielder"}},
	{"id":"3","fullName":"A. Smith","displayName":"A. Smith","position":{"abbreviation":"CB","displayName":"Centre-Back"}}
]}}`

const bournemouthRosterFixture = `{"team":{"id":"349","athletes":[
	{"id":"5","fullName":"Neto","displayName":"Neto","position":{"abbreviation":"G","displayName":"Goalkeeper"}},
	{"id":"6","fullName":"","displayName":"","position":{"abbreviation":"","displayName":""}}
]}}`

const scoreboardDay12 = `{"leagues":[{"name":"English Premier League"}],"events":[
	{"id":"671001","date":"2023-08-12T14:00Z","venue":{"fullName":"Emirates Stadium"},"status":{"type":{"name":"STATUS_FULL_TIME","state":"post"}},"competitions":[{"venue":{"fullName":"Emirates Stadium"},"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":"1"},
		{"homeAway":"away","team":{"id":"349","abbreviation":"BOU","displayName":"AFC Bournemouth"},"score":"3"}]}]},
	{"id":"671002","date":"2023-08-13T11:30Z","status":{"type":{"name":"STATUS_SCHEDULED","state":"pre"}},"competitions":[{"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":""},
		{"homeAway":"away","team":{"id":"349","abbreviation":"BOU","displayName":"AFC Bournemouth"},"score":""}]}]}
]}`

const scoreboardDay14 = `{"leagues":[{"name":"English Premier League"}],"events":[
	{"id":"671003","date":"2023-08-14T12:00Z","status":{"type":{"name":"STATUS_POSTPONED","state":"postponed"}},"competitions":[{"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":""},
		{"homeAway":"away","team":{"id":"349","abbreviation":"BOU","displayName":"AFC Bournemouth"},"score":""}]}]},
	{"id":"671004","date":"2023-08-14T13:00Z","status":{"type":{"name":"STATUS_CANCELLED","state":"canceled"}},"competitions":[{"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"349","abbreviation":"BOU","displayName":"AFC Bournemouth"},"score":""},
		{"homeAway":"away","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":""}]}]},
	{"id":"671005","date":"2023-08-14T14:00Z","status":{"type":{"name":"STATUS_FIRST_HALF","state":"in"}},"competitions":[{"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":"1"},
		{"homeAway":"away","team":{"id":"349","abbreviation":"BOU","displayName":"AFC Bournemouth"},"score":"0"}]}]},
	{"id":"671006","date":"2023-08-14T15:00Z","status":{"type":{"name":"STATUS_WEIRD","state":"zzz"}},"competitions":[{"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":"2"},
		{"homeAway":"away","team":{"id":"349","abbreviation":"BOU","displayName":"AFC Bournemouth"},"score":"1"}]}]}
]}`

const scoreboardDay15 = `{"leagues":[{"name":"English Premier League"}],"events":[
	{"date":"2023-08-15T12:00Z","status":{"type":{"name":"STATUS_SCHEDULED","state":"pre"}},"competitions":[{"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":""},
		{"homeAway":"away","team":{"id":"349","abbreviation":"BOU","displayName":"AFC Bournemouth"},"score":""}]}]},
	{"id":"671007","date":"recently-ish","status":{"type":{"name":"STATUS_SCHEDULED","state":"pre"}},"competitions":[{"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":""},
		{"homeAway":"away","team":{"id":"349","abbreviation":"BOU","displayName":"AFC Bournemouth"},"score":""}]}]},
	{"id":"671008","status":{"type":{"name":"STATUS_SCHEDULED","state":"pre"}},"competitions":[{"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":""},
		{"homeAway":"away","team":{"id":"349","abbreviation":"BOU","displayName":"AFC Bournemouth"},"score":""}]}]},
	{"id":"671009","date":"2023-08-15T16:00Z","status":{"type":{"name":"STATUS_FULL_TIME","state":"post"}},"competitions":[{"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":"2"},
		{"homeAway":"away","team":{"id":"99999","displayName":"Luton Town"},"score":"1"}]}]},
	{"id":"671010","date":"2023-08-15T17:00Z","status":{"type":{"name":"STATUS_SCHEDULED","state":"pre"}},"competitions":[{"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":""}]}]},
	{"id":"671011","date":"2023-08-15T19:00+01:00","status":{"type":{"name":"STATUS_SCHEDULED","state":"pre"}},"competitions":[{"season":2023,"competitors":[
		{"homeAway":"home","team":{"id":"349","abbreviation":"BOU","displayName":"AFC Bournemouth"},"score":""},
		{"homeAway":"away","team":{"id":"359","abbreviation":"ARS","displayName":"Arsenal"},"score":""}]}]}
]}`

const emptyScoreboard = `{"leagues":[],"events":[]}`

const teamsPath = "/apis/site/v2/sports/soccer/eng.1/teams"
const scoreboardPath = "/apis/site/v2/sports/soccer/eng.1/scoreboard"
const seasonPath = "/v2/sports/soccer/leagues/eng.1/seasons/2023"

func newFakeServer(t *testing.T, router func(r *http.Request) string) (*fakeAPI, *httptest.Server) {
	t.Helper()
	api := &fakeAPI{counts: map[string]int{}, router: router}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		api.recordCall(r)
		payload := api.router(r)
		api.mu.Unlock()
		if payload == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(server.Close)
	return api, server
}

func (api *fakeAPI) recordCall(r *http.Request) {
	path := r.URL.Path
	switch {
	case path == teamsPath:
		api.counts["teams-listing"]++
	case strings.HasPrefix(path, "/apis/site/v2/sports/soccer/eng.1/teams/"):
		parts := strings.Split(strings.TrimPrefix(path, "/apis/site/v2/sports/soccer/eng.1/teams/"), "/")
		api.counts["roster:"+parts[0]]++
	case path == seasonPath:
		api.counts["season"]++
	case path == scoreboardPath:
		api.counts["scoreboard:"+r.URL.Query().Get("dates")]++
	default:
		api.counts["unrouted:"+path]++
	}
}

func defaultRouter(r *http.Request) string {
	path := r.URL.Path
	switch {
	case path == teamsPath:
		return teamsFixture
	case strings.HasPrefix(path, "/apis/site/v2/sports/soccer/eng.1/teams/"):
		parts := strings.Split(strings.TrimPrefix(path, "/apis/site/v2/sports/soccer/eng.1/teams/"), "/")
		if len(parts) != 1 {
			return ""
		}
		switch parts[0] {
		case "359":
			return arsenalRosterFixture
		case "349":
			return bournemouthRosterFixture
		}
		return ""
	case path == seasonPath:
		return seasonWindowFixture
	case path == scoreboardPath:
		switch r.URL.Query().Get("dates") {
		case "20230812":
			return scoreboardDay12
		case "20230814":
			return scoreboardDay14
		case "20230815":
			return scoreboardDay15
		}
		return emptyScoreboard
	}
	return ""
}

var testStreamDef = extract.SourceDef{
	ID:      "espn-epl",
	Type:    SourceType,
	Streams: []string{"leagues", "teams", "players", "events"},
	Params: map[string]string{
		"sport":  "soccer",
		"league": "eng.1",
		"code":   "EPL",
		"season": "2023",
	},
}

type testSource struct {
	*Source
	server *httptest.Server
	waits  *waitRecorder
}

func newTestSource(t *testing.T, def extract.SourceDef, router func(r *http.Request) string) *testSource {
	t.Helper()
	recorder := &waitRecorder{}
	_, server := newFakeServer(t, router)
	source := &Source{
		def:        def,
		params:     parseParamsOrDefault(t, def),
		client:     newClientWithWait(Options{SiteBaseURL: server.URL, CoreBaseURL: server.URL}, 0, recorder.wait),
		logger:     testLogger(t),
		teamByID:   map[string]teamRef{},
		teamByName: map[string]string{},
		warned:     map[string]bool{},
	}
	return &testSource{Source: source, server: server, waits: recorder}
}

func parseParamsOrDefault(t *testing.T, def extract.SourceDef) Params {
	t.Helper()
	params, err := parseParams(def)
	if err != nil {
		t.Fatalf("parseParams: %v", err)
	}
	return params
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newSourceAt(t *testing.T, def extract.SourceDef, baseURL string, recorder *waitRecorder) *Source {
	t.Helper()
	return &Source{
		def:        def,
		params:     parseParamsOrDefault(t, def),
		client:     newClientWithWait(Options{SiteBaseURL: baseURL, CoreBaseURL: baseURL}, 0, recorder.wait),
		logger:     testLogger(t),
		teamByID:   map[string]teamRef{},
		teamByName: map[string]string{},
		warned:     map[string]bool{},
	}
}

func walkStream(t *testing.T, source extract.Source, stream string) []extract.Batch {
	t.Helper()
	var batches []extract.Batch
	cursor := map[string]any(nil)
	for page := 0; page < 40; page++ {
		batch, err := source.Fetch(context.Background(), stream, cursor)
		if err != nil {
			t.Fatalf("fetch %s page %d: %v", stream, page, err)
		}
		batches = append(batches, batch)
		if batch.Done {
			return batches
		}
		cursor = batch.Cursor
	}
	t.Fatalf("stream %s failed to terminate", stream)
	return nil
}

func rowsOfKind(batches []extract.Batch, kind model.Kind) []model.Row {
	var rows []model.Row
	for _, batch := range batches {
		for _, row := range batch.Rows {
			if row.Kind == kind {
				rows = append(rows, row)
			}
		}
	}
	return rows
}

func countsOf(api *fakeAPI) map[string]int {
	api.mu.Lock()
	defer api.mu.Unlock()
	out := map[string]int{}
	for key, value := range api.counts {
		out[key] = value
	}
	return out
}

func mustCount(t *testing.T, counts map[string]int, key string, want int) {
	t.Helper()
	if got := counts[key]; got != want {
		t.Errorf("count[%q] = %d, want %d", key, got, want)
	}
}

func terminalRefetch(t *testing.T, source extract.Source, stream string, batch extract.Batch) {
	t.Helper()
	cursor := batch.Cursor
	if cursor == nil {
		t.Fatalf("terminal batch for %s has nil cursor", stream)
	}
	tail, err := source.Fetch(context.Background(), stream, cursor)
	if err != nil {
		t.Fatalf("terminal refetch %s: %v", stream, err)
	}
	if !tail.Done || len(tail.Rows) != 0 {
		t.Errorf("terminal refetch = done %v rows %d, want done true 0 rows", tail.Done, len(tail.Rows))
	}
}

func TestFullWalkAgainstFakeAPI(t *testing.T) {
	api, server := newFakeServer(t, defaultRouter)
	source := newSourceAt(t, testStreamDef, server.URL, &waitRecorder{})

	leagueBatch := walkStream(t, source, "leagues")
	if len(leagueBatch) != 1 {
		t.Fatalf("leagues pages = %d, want 1", len(leagueBatch))
	}
	leagues := rowsOfKind(leagueBatch, model.KindLeague)
	if len(leagues) != 1 {
		t.Fatalf("league rows = %d, want 1", len(leagues))
	}
	league := leagues[0].League
	if league.Code != "EPL" || league.Name != "English Premier League" || league.Sport != "soccer" {
		t.Errorf("league row = %+v", league)
	}
	if leagueBatch[0].Done != true {
		t.Errorf("leagues batch must be terminal")
	}

	teamBatch := walkStream(t, source, "teams")
	if len(teamBatch) != 1 {
		t.Fatalf("teams pages = %d, want 1", len(teamBatch))
	}
	teams := rowsOfKind(teamBatch, model.KindTeam)
	if len(teams) != 2 {
		t.Fatalf("team rows = %d, want 2", len(teams))
	}
	codes := map[string]string{}
	for _, row := range teams {
		codes[row.Team.Code] = row.Team.Name
		if row.Team.LeagueCode != "EPL" {
			t.Errorf("team %s league = %q", row.Team.Code, row.Team.LeagueCode)
		}
	}
	if codes["ARS"] != "Arsenal" || codes["afc-bournemouth"] != "AFC Bournemouth" {
		t.Errorf("team codes = %v", codes)
	}
	for _, row := range teams {
		expectedAbbreviation := ""
		if row.Team.Code == "ARS" {
			expectedAbbreviation = "ARS"
		}
		if row.Team.Abbreviation != expectedAbbreviation {
			t.Errorf("team %s abbreviation = %q, want %q", row.Team.Code, row.Team.Abbreviation, expectedAbbreviation)
		}
	}

	playerBatch := walkStream(t, source, "players")
	if len(playerBatch) != 2 {
		t.Fatalf("players pages = %d, want 2 (page 0 and terminal-page-with-rows)", len(playerBatch))
	}
	players := rowsOfKind(playerBatch, model.KindPlayer)
	playerCodes := map[string]string{}
	for _, row := range players {
		player := row.Player
		playerCodes[player.Code] = player.FullName
		if player.LeagueCode != "EPL" {
			t.Errorf("player %s league = %q", player.Code, player.LeagueCode)
		}
		if player.TeamCode != "ARS" && player.TeamCode != "afc-bournemouth" {
			t.Errorf("player %s team = %q", player.Code, player.TeamCode)
		}
		if !player.Active {
			t.Errorf("player %s must be active", player.Code)
		}
	}
	if playerCodes["bukayo-saka"] != "Bukayo Saka" {
		t.Errorf("saka row missing: %v", playerCodes)
	}
	if playerCodes["a-smith"] != "A Smith" || playerCodes["a-smith-3"] != "A. Smith" {
		t.Errorf("collision-suffixed codes wrong: %v", playerCodes)
	}
	if playerCodes["neto"] != "Neto" {
		t.Errorf("neto row missing: %v", playerCodes)
	}
	if rowsOfKind(playerBatch, model.KindPlayer)[0].Player.Position != "Right Winger" {
		t.Errorf("first player position = %q, want Right Winger", players[0].Player.Position)
	}

	eventBatch := walkStream(t, source, "events")
	if len(eventBatch) != 11 {
		t.Fatalf("events pages = %d, want 11 (one per window day)", len(eventBatch))
	}
	gameRows := rowsOfKind(eventBatch, model.KindGame)
	if len(gameRows) != 8 {
		t.Fatalf("game rows = %d, want 8", len(gameRows))
	}
	games := map[string]*model.Game{}
	for _, row := range gameRows {
		games[row.Game.GameCode] = row.Game
	}

	game := games["671001"]
	if game == nil || game.Status != "final" || game.HomeScore == nil || *game.HomeScore != 1 ||
		game.AwayScore == nil || *game.AwayScore != 3 || game.HomeTeamCode != "ARS" ||
		game.AwayTeamCode != "afc-bournemouth" || game.Venue != "Emirates Stadium" ||
		game.Season != "2023" {
		t.Errorf("game 671001 = %+v", game)
	}
	if !game.StartsAt.Equal(time.Date(2023, time.August, 12, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("game 671001 starts_at = %v", game.StartsAt)
	}

	game = games["671002"]
	if game == nil || game.Status != "scheduled" || game.HomeScore != nil || game.AwayScore != nil ||
		game.HomeTeamCode != "ARS" || game.AwayTeamCode != "afc-bournemouth" {
		t.Errorf("game 671002 = %+v", game)
	}
	if !game.StartsAt.Equal(time.Date(2023, time.August, 13, 11, 30, 0, 0, time.UTC)) {
		t.Errorf("game 671002 starts_at = %v", game.StartsAt)
	}

	game = games["671003"]
	if game == nil || game.Status != "postponed" {
		t.Errorf("game 671003 = %+v", game)
	}
	game = games["671004"]
	if game == nil || game.Status != "canceled" {
		t.Errorf("game 671004 = %+v", game)
	}
	game = games["671005"]
	if game == nil || game.Status != "live" || game.HomeScore == nil || *game.HomeScore != 1 ||
		game.AwayScore == nil || *game.AwayScore != 0 {
		t.Errorf("game 671005 = %+v", game)
	}
	game = games["671006"]
	if game == nil || game.Status != "final" || game.HomeScore == nil || *game.HomeScore != 2 {
		t.Errorf("game 671006 = %+v", game)
	}

	fallbackTeams := rowsOfKind(eventBatch, model.KindTeam)
	if len(fallbackTeams) != 1 {
		t.Fatalf("fallback team rows = %d, want 1", len(fallbackTeams))
	}
	if fallbackTeams[0].Team.Code != "luton-town" || fallbackTeams[0].Team.Name != "Luton Town" {
		t.Errorf("fallback team = %+v", fallbackTeams[0].Team)
	}

	finalBatch := eventBatch[len(eventBatch)-1]
	date, ok := finalBatch.Cursor["date"]
	if !ok || date != "20230823" {
		t.Errorf("final events cursor = %v, want date 20230823", finalBatch.Cursor)
	}

	counts := countsOf(api)
	mustCount(t, counts, "teams-listing", 1)
	mustCount(t, counts, "season", 1)
	mustCount(t, counts, "roster:359", 1)
	mustCount(t, counts, "roster:349", 1)
	for day := 12; day <= 22; day++ {
		mustCount(t, counts, fmt.Sprintf("scoreboard:202308%02d", day), 1)
	}
	totalScoreboard := 0
	for key, value := range counts {
		if strings.HasPrefix(key, "scoreboard:") {
			totalScoreboard += value
		}
	}
	if totalScoreboard != 11 {
		t.Errorf("scoreboard calls = %d, want 11", totalScoreboard)
	}
	if len(counts) != 15 {
		t.Errorf("unexpected extra call endpoints: %v", counts)
	}

	terminalRefetch(t, source, "leagues", leagueBatch[0])
	terminalRefetch(t, source, "teams", teamBatch[0])
	terminalRefetch(t, source, "events", eventBatch[len(eventBatch)-1])
	afterTerminal := countsOf(api)
	for key, want := range counts {
		if afterTerminal[key] != want {
			t.Errorf("terminal refetch changed count[%q] from %d to %d", key, want, afterTerminal[key])
		}
	}
}

func TestPlayersResumeFromStoredCursor(t *testing.T) {
	api, server := newFakeServer(t, defaultRouter)
	source := newSourceAt(t, testStreamDef, server.URL, &waitRecorder{})
	resumedSource := newSourceAt(t, testStreamDef, server.URL, &waitRecorder{})

	_, err := source.Fetch(context.Background(), "teams", nil)
	if err != nil {
		t.Fatalf("teams: %v", err)
	}
	first, err := source.Fetch(context.Background(), "players", nil)
	if err != nil {
		t.Fatalf("players page 0: %v", err)
	}
	if first.Done {
		t.Fatalf("page 0 must not be terminal")
	}
	stored, ok := first.Cursor["idx"].(int)
	if !ok || stored != 1 {
		t.Fatalf("page 0 cursor = %v", first.Cursor)
	}

	batch, err := resumedSource.Fetch(context.Background(), "players", first.Cursor)
	if err != nil {
		t.Fatalf("resume fetch: %v", err)
	}
	if !batch.Done {
		t.Fatalf("resume page must be terminal, got %v", batch)
	}
	if len(batch.Rows) != 1 {
		t.Fatalf("resume rows = %d, want 1 (roster of the remaining team)", len(batch.Rows))
	}
	player := batch.Rows[0].Player
	if player.Code != "neto" || player.TeamCode != "afc-bournemouth" {
		t.Errorf("resume player = %+v", player)
	}

	counts := countsOf(api)
	if got := counts["roster:349"]; got != 1 {
		t.Errorf("resume must page the remaining roster once: %v", got)
	}
	if got := counts["roster:359"]; got != 1 {
		t.Errorf("resume must not refetch already-paged rosters: %v", got)
	}
	if got := counts["teams-listing"]; got != 2 {
		t.Errorf("resume must rebuild the team listing once per process: %v", got)
	}
}

func TestEventsResumeFromStoredCursor(t *testing.T) {
	api, server := newFakeServer(t, defaultRouter)
	source := newSourceAt(t, testStreamDef, server.URL, &waitRecorder{})

	for _, dates := range []string{"12", "13", "14", "15", "16", "17", "18"} {
		batch, err := source.Fetch(context.Background(), "events", map[string]any{"date": "202308" + dates})
		if err != nil {
			t.Fatalf("events simulation day %s: %v", dates, err)
		}
		if batch.Done {
			t.Fatalf("day %s must not be terminal", dates)
		}
	}
	batches := 0
	cursor := map[string]any{"date": "20230819"}
	for page := 0; page < 10; page++ {
		batch, err := source.Fetch(context.Background(), "events", cursor)
		if err != nil {
			t.Fatalf("resume fetch page %d: %v", page, err)
		}
		batches++
		if batch.Done {
			if batch.Cursor["date"] != "20230823" {
				t.Fatalf("terminal cursor = %v, want 20230823", batch.Cursor)
			}
			break
		}
		cursor = batch.Cursor
	}
	if batches < 2 {
		t.Fatalf("resume must walk several days, got %d", batches)
	}
	counts := countsOf(api)
	for day := 12; day <= 22; day++ {
		mustCount(t, counts, fmt.Sprintf("scoreboard:202308%02d", day), 1)
	}
	if counts["season"] != 1 {
		t.Errorf("season window calls = %d, want 1", counts["season"])
	}
}

func TestEventsCursorValidation(t *testing.T) {
	source := newTestSource(t, testStreamDef, defaultRouter)
	cases := []struct {
		name   string
		cursor map[string]any
	}{
		{"number date", map[string]any{"date": 12.5}},
		{"missing date", map[string]any{}},
		{"garbage date", map[string]any{"date": "not-a-date"}},
	}
	for _, testCase := range cases {
		if _, err := source.Fetch(context.Background(), "events", testCase.cursor); err == nil {
			t.Errorf("%s must fail", testCase.name)
		}
	}
}

func TestEventsRefreshParksAtTodayWhileSeasonRuns(t *testing.T) {
	api, server := newFakeServer(t, defaultRouter)
	source := newSourceAt(t, testStreamDef, server.URL, &waitRecorder{})
	source.now = func() time.Time { return time.Date(2023, time.August, 16, 15, 0, 0, 0, time.UTC) }

	cursor := map[string]any(nil)
	batches := 0
	var final extract.Batch
	for page := 0; page < 10; page++ {
		batch, err := source.Fetch(context.Background(), "events", cursor)
		if err != nil {
			t.Fatalf("events fetch page %d: %v", page, err)
		}
		batches++
		if batch.Done {
			final = batch
			break
		}
		cursor = batch.Cursor
	}
	if batches != 5 {
		t.Fatalf("walk pages = %d, want 5 (season window clipped to today)", batches)
	}
	if final.Cursor["date"] != "20230814" {
		t.Errorf("parked cursor = %v, want date 20230814 (today minus the refresh tail)", final.Cursor)
	}

	tail := final.Cursor
	var again extract.Batch
	for page := 0; page < 5; page++ {
		var err error
		again, err = source.Fetch(context.Background(), "events", tail)
		if err != nil {
			t.Fatalf("refresh cycle fetch page %d: %v", page, err)
		}
		if again.Done {
			break
		}
		tail = again.Cursor
	}
	if !again.Done || again.Cursor["date"] != "20230814" {
		t.Errorf("refresh cycle = %v, want tail re-walk terminating at the same parked cursor", again)
	}

	counts := countsOf(api)
	for day := 12; day <= 16; day++ {
		want := 1
		if day >= 14 {
			want = 2
		}
		mustCount(t, counts, fmt.Sprintf("scoreboard:202308%02d", day), want)
	}
	for day := 17; day <= 22; day++ {
		if got := counts[fmt.Sprintf("scoreboard:202308%02d", day)]; got != 0 {
			t.Errorf("future day %d scoreboard called %d times, want 0", day, got)
		}
	}
	if counts["season"] != 1 {
		t.Errorf("season window calls = %d, want 1", counts["season"])
	}
}

func TestEventsCursorWithoutSeasonFailsAtFactory(t *testing.T) {
	factory := Factory(Options{})
	if _, err := factory(extract.SourceDef{
		ID:      "no-season",
		Type:    SourceType,
		Streams: []string{"leagues", "events"},
		Params:  map[string]string{"sport": "soccer", "league": "eng.1", "code": "EPL"},
	}); err == nil {
		t.Errorf("events without season param must fail at the factory")
	}
}

func TestRateLimitedFetchRecoversWithPenalty(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == seasonPath {
			mu.Lock()
			calls++
			call := calls
			mu.Unlock()
			if call == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"slow down"}`))
				return
			}
			_, _ = w.Write([]byte(seasonWindowFixture))
			return
		}
		if r.URL.Path == teamsPath {
			_, _ = w.Write([]byte(teamsFixture))
			return
		}
		if r.URL.Path == scoreboardPath {
			_, _ = w.Write([]byte(emptyScoreboard))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	recorder := &waitRecorder{}
	source := newSourceAt(t, testStreamDef, server.URL, recorder)

	batch, err := source.Fetch(context.Background(), "events", nil)
	if err != nil {
		t.Fatalf("rate-limited fetch: %v", err)
	}
	if batch.Cursor["date"] == nil {
		t.Errorf("batch cursor = %v", batch.Cursor)
	}
	recorder.mu.Lock()
	last := recorder.waits[len(recorder.waits)-1]
	recorder.mu.Unlock()
	if last != penaltyWait {
		t.Errorf("penalty wake = %v, want %v", last, penaltyWait)
	}
}

func TestRateLimitPersistsFailsAfterOnePenalty(t *testing.T) {
	calls := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	recorder := &waitRecorder{}
	source := newSourceAt(t, testStreamDef, server.URL, recorder)

	if _, err := source.Fetch(context.Background(), "events", nil); err == nil {
		t.Fatalf("persistent 429 must fail")
	}
	mu.Lock()
	total := calls
	mu.Unlock()
	if total != 2 {
		t.Errorf("calls = %d, want 2 (request + one penalty retry)", total)
	}
}

func TestPaceReservesInterval(t *testing.T) {
	recorder := &waitRecorder{}
	client := newClientWithWait(Options{SiteBaseURL: "http://unused", CoreBaseURL: "http://unused"}, 5*time.Millisecond, recorder.wait)
	ctx := context.Background()
	if err := client.pace(ctx); err != nil {
		t.Fatalf("first pace: %v", err)
	}
	if err := client.pace(ctx); err != nil {
		t.Fatalf("second pace: %v", err)
	}
	recorder.mu.Lock()
	waits := append([]time.Duration(nil), recorder.waits...)
	recorder.mu.Unlock()
	if len(waits) != 1 || waits[0] < 3*time.Millisecond || waits[0] > 6*time.Millisecond {
		t.Errorf("waits = %v, want one ~5ms interval", waits)
	}
}

func TestPaceHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := newClientWithWait(Options{SiteBaseURL: "http://unused", CoreBaseURL: "http://unused"}, time.Hour, sleepWait)
	if err := client.pace(ctx); err != nil {
		t.Fatalf("first pace should reserve without waiting: %v", err)
	}
	if err := client.pace(ctx); err == nil {
		t.Errorf("second pace on cancelled context must fail")
	}
}

func TestGarbageScoreboardFailsLoudly(t *testing.T) {
	router := func(r *http.Request) string {
		switch r.URL.Path {
		case teamsPath:
			return teamsFixture
		case seasonPath:
			return seasonWindowFixture
		case scoreboardPath:
			return `{"events":`
		}
		return ""
	}
	source := newTestSource(t, testStreamDef, router)
	if _, err := source.Fetch(context.Background(), "events", nil); err == nil {
		t.Fatalf("garbage scoreboard payload must fail loudly")
	}
}

func TestFactoryValidatesParams(t *testing.T) {
	factory := Factory(Options{SiteBaseURL: "http://unused", CoreBaseURL: "http://unused"})

	_, err := factory(extract.SourceDef{ID: "a", Type: SourceType, Streams: []string{"leagues"}})
	if err == nil {
		t.Fatalf("missing params must fail")
	}

	_, err = factory(extract.SourceDef{
		ID:      "b",
		Type:    SourceType,
		Streams: []string{"leagues", "teams"},
		Params:  map[string]string{"sport": "soccerx", "league": "eng.1", "code": "EPL"},
	})
	if err == nil {
		t.Fatalf("unsupported sport must fail")
	}

	_, err = factory(extract.SourceDef{
		ID:      "c",
		Type:    SourceType,
		Streams: []string{"leagues", "teams"},
		Params:  map[string]string{"sport": "soccer", "code": "EPL", "season": "2023"},
	})
	if err == nil {
		t.Fatalf("missing league must fail")
	}

	_, err = factory(extract.SourceDef{
		ID:      "d",
		Type:    SourceType,
		Streams: []string{"leagues", "teams", "players"},
		Params:  map[string]string{"sport": "soccer", "league": "eng.1", "code": "EPL", "season": "2023-2024"},
	})
	if err == nil {
		t.Fatalf("ESPN-style season must fail as invalid")
	}

	_, err = factory(extract.SourceDef{
		ID:      "e",
		Type:    SourceType,
		Streams: []string{"leagues", "teams", "players"},
		Params:  map[string]string{"sport": "soccer", "league": "eng.1", "code": "EPL"},
	})
	if err != nil {
		t.Fatalf("players without season must be allowed: %v", err)
	}

	source, err := factory(extract.SourceDef{
		ID:      "f",
		Type:    SourceType,
		Streams: []string{"leagues", "teams", "players", "events"},
		Params:  map[string]string{"sport": "basketball", "league": "nba", "code": "NBA", "season": "2024"},
	})
	if err != nil {
		t.Fatalf("valid def must build: %v", err)
	}
	if len(source.Def().Streams) != 4 {
		t.Errorf("streams = %v", source.Def().Streams)
	}
}

func TestFactoryRejectsUnknownStreamAtRuntime(t *testing.T) {
	factory := Factory(Options{SiteBaseURL: "http://unused", CoreBaseURL: "http://unused"})
	source, err := factory(extract.SourceDef{
		ID:      "a",
		Type:    SourceType,
		Streams: []string{"leagues"},
		Params:  map[string]string{"sport": "soccer", "league": "eng.1", "code": "EPL"},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := source.Fetch(context.Background(), "stats", nil); err == nil {
		t.Errorf("unknown stream must fail")
	}
}
