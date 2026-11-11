package tsdb

import (
	"context"
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

const (
	leagueFixture = `{"leagues":[{"idLeague":"4328","strLeague":"English Premier League","strSport":"Soccer"}]}`
	teamsFixture  = `{"teams":[
		{"idTeam":"133604","strTeam":"Arsenal","strTeamShort":"ARS"},
		{"idTeam":"133613","strTeam":"Manchester side","strTeamShort":""}
	]}`
	playersArsenalFixture = `{"player":[
		{"idPlayer":"1","idTeam":"133604","strPlayer":"Arteta Coach","strTeam":"Arsenal","strPosition":"Head Coach"},
		{"idPlayer":"2","idTeam":"133604","strPlayer":"Bukayo Saka","strTeam":"Arsenal","strPosition":"Right Winger"},
		{"idPlayer":"3","idTeam":"133604","strPlayer":"A Smith","strTeam":"Arsenal","strPosition":"Midfielder"},
		{"idPlayer":"4","idTeam":"133604","strPlayer":"A. Smith","strTeam":"Arsenal","strPosition":"Defender"}
	]}`
	playersSideFixture = `{"player":[
		{"idPlayer":"5","idTeam":"133613","strPlayer":"Ederson","strTeam":"Manchester side","strPosition":"Goalkeeper"}
	]}`
	eventsFixture = `{"events":[
		{"idEvent":"1818395","strEvent":"Arsenal vs Manchester side","strSeason":"2023-2024","strHomeTeam":"Arsenal","strAwayTeam":"Manchester side","intHomeScore":"1","intAwayScore":"3","dateEvent":"2023-08-11","strTime":"19:00:00","strTimestamp":"2023-08-11T19:00:00","strStatus":"FT","strPostponed":"no","strVenue":"Emirates Stadium"},
		{"idEvent":"1818396","strEvent":"Burnley vs Arsenal","strSeason":"2023-2024","strHomeTeam":"Burnley","strAwayTeam":"Arsenal","intHomeScore":"","intAwayScore":"","dateEvent":"2023-08-12","strTime":"11:30:00","strTimestamp":"","strStatus":"Not Started","strPostponed":"no","strVenue":"Turf Moor"},
		{"idEvent":"1818397","strEvent":"Burnley vs Luton","strSeason":"","strHomeTeam":"Burnley","strAwayTeam":"Luton","intHomeScore":"2","intAwayScore":"1","dateEvent":"2023-08-13","strTime":"14:00:00","strTimestamp":"","strStatus":"X-Unknown-Status","strPostponed":"yes","strVenue":"Turf Moor"},
		{"idEvent":"1818398","strEvent":"Luton vs Burnley","strSeason":"2023-2024","strHomeTeam":"Luton","strAwayTeam":"Burnley","intHomeScore":"2","intAwayScore":"","dateEvent":"2023-08-14","strTime":"","strTimestamp":"2023-08-14T12:00:00+02:00","strStatus":"X-Unknown-Status","strPostponed":"no","strVenue":"Kenilworth Road"},
		{"idEvent":"1818399","strEvent":"Broken fixture","strSeason":"2023-2024","strHomeTeam":"Arsenal","strAwayTeam":"","intHomeScore":"1","intAwayScore":"0","dateEvent":"2023-08-15","strTime":"10:00:00","strTimestamp":"","strStatus":"FT","strPostponed":"no","strVenue":""}
	]}`
)

func newFakeServer(t *testing.T, router func(r *http.Request) string) (*fakeAPI, *httptest.Server) {
	t.Helper()
	api := &fakeAPI{counts: map[string]int{}, router: router}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		endpoint := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		api.counts[endpoint+rSuffix(r)]++
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

func rSuffix(r *http.Request) string {
	if r.URL.RawQuery == "" {
		return ""
	}
	return "?" + r.URL.RawQuery
}

var testStreamDef = extract.SourceDef{
	ID:      "tsdb-epl",
	Type:    SourceType,
	Streams: []string{"leagues", "teams", "players", "events"},
	Params: map[string]string{
		"league_id": "4328",
		"code":      "EPL",
		"season":    "2023-2024",
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
		client:     newClientWithWait(Options{APIKey: "123", BaseURL: server.URL}, 0, recorder.wait),
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
		client:     newClientWithWait(Options{APIKey: "123", BaseURL: baseURL}, 0, recorder.wait),
		logger:     testLogger(t),
		teamByID:   map[string]teamRef{},
		teamByName: map[string]string{},
		warned:     map[string]bool{},
	}
}

func routerFor(t *testing.T) func(r *http.Request) string {
	t.Helper()
	return func(r *http.Request) string {
		switch r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:] {
		case "lookupleague.php":
			if r.URL.Query().Get("id") != "4328" {
				return ""
			}
			return leagueFixture
		case "search_all_teams.php":
			if r.URL.Query().Get("l") != "English Premier League" {
				return ""
			}
			return teamsFixture
		case "lookup_all_players.php":
			switch r.URL.Query().Get("id") {
			case "133604":
				return playersArsenalFixture
			case "133613":
				return playersSideFixture
			}
			return ""
		case "eventsseason.php":
			if r.URL.Query().Get("id") == "4328" && r.URL.Query().Get("s") == "2023-2024" {
				return eventsFixture
			}
			return ""
		}
		return ""
	}
}

func walkStream(t *testing.T, source extract.Source, stream string) []extract.Batch {
	t.Helper()
	var batches []extract.Batch
	cursor := map[string]any(nil)
	for page := 0; page < 20; page++ {
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
	api, server := newFakeServer(t, routerFor(t))
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
	if leagueBatch[0].Cursor["done"] != true {
		t.Errorf("leagues terminal cursor = %v", leagueBatch[0].Cursor)
	}

	teamBatch := walkStream(t, source, "teams")
	if len(teamBatch) != 1 {
		t.Fatalf("teams pages = %d, want 1", len(teamBatch))
	}
	teams := rowsOfKind(teamBatch, model.KindTeam)
	if len(teams) != 2 {
		t.Fatalf("team rows = %d, want 2 listing teams", len(teams))
	}
	codes := map[string]string{}
	for _, row := range teams {
		codes[row.Team.Code] = row.Team.Name
	}
	if codes["ARS"] != "Arsenal" || codes["manchester-side"] != "Manchester side" {
		t.Errorf("team codes = %v", codes)
	}
	for _, row := range teams {
		expectedAbbr := ""
		if row.Team.Code == "ARS" {
			expectedAbbr = "ARS"
		}
		if row.Team.Abbreviation != expectedAbbr {
			t.Errorf("team %s abbreviation = %q, want %q", row.Team.Code, row.Team.Abbreviation, expectedAbbr)
		}
		if row.Team.LeagueCode != "EPL" {
			t.Errorf("team %s league = %q", row.Team.Code, row.Team.LeagueCode)
		}
	}

	playerBatch := walkStream(t, source, "players")
	if len(playerBatch) != 2 {
		t.Fatalf("players pages = %d, want 2 (one per team)", len(playerBatch))
	}
	players := rowsOfKind(playerBatch, model.KindPlayer)
	playerCodes := map[string]string{}
	for _, row := range players {
		player := row.Player
		playerCodes[player.Code] = player.FullName
		if player.LeagueCode != "EPL" {
			t.Errorf("player %s league = %q", player.Code, player.LeagueCode)
		}
		if player.TeamCode != "ARS" && player.TeamCode != "manchester-side" {
			t.Errorf("player %s team = %q", player.Code, player.TeamCode)
		}
	}
	if playerCodes["bukayo-saka"] != "Bukayo Saka" {
		t.Errorf("saka row missing: %v", playerCodes)
	}
	if playerCodes["a-smith"] != "A Smith" || playerCodes["a-smith-4"] != "A. Smith" {
		t.Errorf("collision-suffixed codes wrong: %v", playerCodes)
	}
	if _, coached := playerCodes["arteta-coach"]; coached {
		t.Errorf("coach must be filtered out: %v", playerCodes)
	}

	eventBatch := walkStream(t, source, "events")
	if len(eventBatch) != 1 {
		t.Fatalf("events pages = %d, want 1", len(eventBatch))
	}
	gameRows := rowsOfKind(eventBatch, model.KindGame)
	if len(gameRows) != 4 {
		t.Fatalf("game rows = %d, want 4", len(gameRows))
	}
	games := map[string]*model.Game{}
	for _, row := range gameRows {
		games[row.Game.GameCode] = row.Game
	}

	game := games["1818395"]
	if game == nil || game.Status != "final" || game.HomeScore == nil || *game.HomeScore != 1 ||
		game.AwayScore == nil || *game.AwayScore != 3 || game.HomeTeamCode != "ARS" ||
		game.AwayTeamCode != "manchester-side" || game.Venue != "Emirates Stadium" ||
		game.Season != "2023-2024" {
		t.Errorf("game 1818395 = %+v", game)
	}
	if !game.StartsAt.Equal(time.Date(2023, time.August, 11, 19, 0, 0, 0, time.UTC)) {
		t.Errorf("game 1818395 starts_at = %v", game.StartsAt)
	}

	game = games["1818396"]
	if game == nil || game.Status != "scheduled" || game.HomeScore != nil || game.AwayScore != nil ||
		game.HomeTeamCode != "burnley" || game.AwayTeamCode != "ARS" {
		t.Errorf("game 1818396 = %+v", game)
	}
	if !game.StartsAt.Equal(time.Date(2023, time.August, 12, 11, 30, 0, 0, time.UTC)) {
		t.Errorf("game 1818396 starts_at = %v", game.StartsAt)
	}

	game = games["1818397"]
	if game == nil || game.Status != "postponed" || game.HomeScore == nil || *game.HomeScore != 2 ||
		game.AwayScore == nil || *game.AwayScore != 1 || game.Season != "2023-2024" {
		t.Errorf("game 1818397 = %+v", game)
	}

	game = games["1818398"]
	if game == nil || game.Status != "final" ||
		!game.StartsAt.Equal(time.Date(2023, time.August, 14, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("game 1818398 = %+v", game)
	}

	fallbackTeams := rowsOfKind(eventBatch, model.KindTeam)
	if len(fallbackTeams) != 2 {
		t.Fatalf("fallback team rows = %d, want 2 (deduped)", len(fallbackTeams))
	}
	fallbackCodes := map[string]string{}
	for _, row := range fallbackTeams {
		fallbackCodes[row.Team.Code] = row.Team.Name
	}
	if fallbackCodes["burnley"] != "Burnley" || fallbackCodes["luton"] != "Luton" {
		t.Errorf("fallback teams = %v", fallbackCodes)
	}
	for _, row := range fallbackTeams {
		if row.Team.Abbreviation != "" {
			t.Errorf("fallback team %s must have no abbreviation, got %q", row.Team.Code, row.Team.Abbreviation)
		}
	}

	counts := countsOf(api)
	mustCount(t, counts, "lookupleague.php?id=4328", 1)
	mustCount(t, counts, "search_all_teams.php?l=English+Premier+League", 1)
	mustCount(t, counts, "lookup_all_players.php?id=133604", 1)
	mustCount(t, counts, "lookup_all_players.php?id=133613", 1)
	mustCount(t, counts, "eventsseason.php?id=4328&s=2023-2024", 1)
	if len(counts) != 5 {
		t.Errorf("unexpected extra call endpoints: %v", counts)
	}

	terminalRefetch(t, source, "leagues", leagueBatch[0])
	terminalRefetch(t, source, "teams", teamBatch[0])
	terminalRefetch(t, source, "events", eventBatch[0])
	afterTerminal := countsOf(api)
	for key, want := range counts {
		if afterTerminal[key] != want {
			t.Errorf("terminal refetch changed count[%q] from %d to %d", key, want, afterTerminal[key])
		}
	}
}

func TestPlayersResumeFromStoredCursor(t *testing.T) {
	api, server := newFakeServer(t, routerFor(t))
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
		t.Fatalf("resume rows = %d, want 1", len(batch.Rows))
	}
	player := batch.Rows[0].Player
	if player.Code != "ederson" || player.TeamCode != "manchester-side" {
		t.Errorf("resume player = %+v", player)
	}

	counts := countsOf(api)
	if got := counts["lookup_all_players.php?id=133613"]; got != 1 {
		t.Errorf("resume must page the remaining roster once: %v", got)
	}
	if got := counts["lookup_all_players.php?id=133604"]; got != 1 {
		t.Errorf("resume must not refetch already-paged rosters: %v", got)
	}
	if got := counts["search_all_teams.php?l=English+Premier+League"]; got != 2 {
		t.Errorf("resume must rebuild the team listing once per process: %v", got)
	}
	if got := counts["lookupleague.php?id=4328"]; got != 2 {
		t.Errorf("resume must resolve the league name once per process: %v", got)
	}
}

func TestRateLimitedFetchRecoversWithPenalty(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 1 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow down"}`))
			return
		}
		_, _ = w.Write([]byte(leagueFixture))
	}))
	t.Cleanup(server.Close)

	recorder := &waitRecorder{}
	source := newSourceAt(t, testStreamDef, server.URL, recorder)

	batch, err := source.Fetch(context.Background(), "leagues", nil)
	if err != nil {
		t.Fatalf("rate-limited fetch: %v", err)
	}
	if len(batch.Rows) != 1 {
		t.Fatalf("rows after recovery = %d, want 1", len(batch.Rows))
	}
	recorder.mu.Lock()
	penalty := len(recorder.waits) > 0 && recorder.waits[len(recorder.waits)-1] == penaltyWait
	recorder.mu.Unlock()
	if !penalty {
		t.Errorf("penalty wake was not requested: %v", recorder.waits)
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

	if _, err := source.Fetch(context.Background(), "leagues", nil); err == nil {
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
	client := newClientWithWait(Options{APIKey: "123", BaseURL: "http://unused"}, 5*time.Millisecond, recorder.wait)
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
	client := newClientWithWait(Options{APIKey: "123", BaseURL: "http://unused"}, time.Hour, sleepWait)
	if err := client.pace(ctx); err != nil {
		t.Fatalf("first pace should reserve without waiting: %v", err)
	}
	if err := client.pace(ctx); err == nil {
		t.Errorf("second pace on cancelled context must fail")
	}
}

func TestFactoryValidatesParams(t *testing.T) {
	factory := Factory(Options{APIKey: "123", BaseURL: "http://unused"})

	_, err := factory(extract.SourceDef{ID: "a", Type: SourceType, Streams: []string{"leagues"}})
	if err == nil {
		t.Fatalf("missing params must fail")
	}

	_, err = factory(extract.SourceDef{
		ID:      "b",
		Type:    SourceType,
		Streams: []string{"leagues", "teams", "players"},
		Params:  map[string]string{"league_id": "4328"},
	})
	if err == nil {
		t.Fatalf("missing code must fail")
	}

	_, err = factory(extract.SourceDef{
		ID:      "c",
		Type:    SourceType,
		Streams: []string{"leagues", "events"},
		Params:  map[string]string{"league_id": "4328", "code": "EPL"},
	})
	if err == nil {
		t.Fatalf("missing season for events stream must fail")
	}

	source, err := factory(extract.SourceDef{
		ID:      "d",
		Type:    SourceType,
		Streams: []string{"leagues", "teams", "players", "events"},
		Params:  map[string]string{"league_id": "4328", "code": "EPL", "season": "2023-2024"},
	})
	if err != nil {
		t.Fatalf("valid def must build: %v", err)
	}
	if len(source.Def().Streams) != 4 {
		t.Errorf("streams = %v", source.Def().Streams)
	}
}

func TestFactoryRejectsUnknownStreamAtRuntime(t *testing.T) {
	factory := Factory(Options{APIKey: "123", BaseURL: "http://unused"})
	source, err := factory(extract.SourceDef{
		ID:      "a",
		Type:    SourceType,
		Streams: []string{"leagues"},
		Params:  map[string]string{"league_id": "4328", "code": "EPL"},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := source.Fetch(context.Background(), "stats", nil); err == nil {
		t.Errorf("unknown stream must fail")
	}
}

func TestUnknownStatusesAndBadPayloadsFailLoudly(t *testing.T) {
	router := func(r *http.Request) string {
		endpoint := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		switch endpoint {
		case "lookupleague.php":
			return leagueFixture
		case "search_all_teams.php":
			return teamsFixture
		case "eventsseason.php":
			return `{"events":null}`
		}
		return ""
	}
	source := newTestSource(t, testStreamDef, router)
	if _, err := source.Fetch(context.Background(), "events", nil); err == nil {
		t.Fatalf("null events payload must fail loudly")
	}
}
