package espn

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"sports-analytics/ingestor/extract"
	"sports-analytics/ingestor/model"
)

const (
	streamLeagues = "leagues"
	streamTeams   = "teams"
	streamPlayers = "players"
	streamEvents  = "events"

	supportedSoccer     = "soccer"
	supportedBasketball = "basketball"

	refreshTailDays = 2
)

var supportedSports = map[string]bool{supportedSoccer: true, supportedBasketball: true}

type Params struct {
	Sport  string
	League string
	Code   string
	Season string
}

func parseParams(def extract.SourceDef) (Params, error) {
	params := Params{
		Sport:  strings.ToLower(strings.TrimSpace(def.Params["sport"])),
		League: strings.TrimSpace(def.Params["league"]),
		Code:   strings.TrimSpace(def.Params["code"]),
		Season: strings.TrimSpace(def.Params["season"]),
	}
	if params.Sport == "" {
		return Params{}, fmt.Errorf("param sport is required")
	}
	if !supportedSports[params.Sport] {
		return Params{}, fmt.Errorf("param sport %q is not supported, use soccer or basketball", params.Sport)
	}
	if params.League == "" {
		return Params{}, fmt.Errorf("param league is required")
	}
	if params.Code == "" {
		return Params{}, fmt.Errorf("param code is required")
	}
	if params.Season != "" {
		if _, err := parseSeasonYear(params.Season); err != nil {
			return Params{}, err
		}
	}
	if hasStream(def.Streams, streamEvents) && params.Season == "" {
		return Params{}, fmt.Errorf("param season is required for the %s stream", streamEvents)
	}
	return params, nil
}

func hasStream(streams []string, stream string) bool {
	for _, candidate := range streams {
		if candidate == stream {
			return true
		}
	}
	return false
}

type seasonWindow struct {
	start time.Time
	end   time.Time
}

type teamRef struct {
	id    string
	code  string
	short string
	name  string
}

type Source struct {
	def    extract.SourceDef
	params Params

	client *client
	logger *slog.Logger

	mu         sync.Mutex
	leagueName string
	teamRefs   []teamRef
	teamByID   map[string]teamRef
	teamByName map[string]string
	warned     map[string]bool
	window     *seasonWindow
	now        func() time.Time
}

func Factory(options Options) extract.Factory {
	return func(def extract.SourceDef) (extract.Source, error) {
		params, err := parseParams(def)
		if err != nil {
			return nil, fmt.Errorf("sources: espn %q: %w", def.ID, err)
		}
		return &Source{
			def:        def,
			params:     params,
			client:     newClient(options),
			logger:     slog.Default(),
			teamByID:   map[string]teamRef{},
			teamByName: map[string]string{},
			warned:     map[string]bool{},
			now:        time.Now,
		}, nil
	}
}

func (s *Source) Def() extract.SourceDef { return s.def }

func (s *Source) Fetch(ctx context.Context, stream string, cursor map[string]any) (extract.Batch, error) {
	switch stream {
	case streamLeagues:
		return s.fetchLeagues(ctx, stream, cursor)
	case streamTeams:
		return s.fetchTeams(ctx, stream, cursor)
	case streamPlayers:
		return s.fetchPlayers(ctx, stream, cursor)
	case streamEvents:
		return s.fetchEvents(ctx, stream, cursor)
	default:
		return extract.Batch{}, &extract.UnretryableError{
			Err: fmt.Errorf("sources: espn %q has no stream %q", s.def.ID, stream),
		}
	}
}

func (s *Source) fetchLeagues(ctx context.Context, stream string, cursor map[string]any) (extract.Batch, error) {
	if resumed(cursor) {
		return s.finished(stream, cursor), nil
	}
	refs, err := s.ensureTeams(ctx)
	if err != nil {
		return extract.Batch{}, err
	}
	if len(refs) == 0 {
		return extract.Batch{}, &extract.UnretryableError{
			Err: fmt.Errorf("sources: espn %q: league listing for %s/%s is empty", s.def.ID, s.params.Sport, s.params.League),
		}
	}
	name, err := s.leagueListingName(ctx)
	if err != nil {
		return extract.Batch{}, err
	}
	rows := []model.Row{{
		Kind:   model.KindLeague,
		League: &model.League{Code: s.params.Code, Name: name, Sport: s.params.Sport},
	}}
	return extract.Batch{Stream: stream, Cursor: map[string]any{"done": true}, Rows: rows, Done: true}, nil
}

func (s *Source) fetchTeams(ctx context.Context, stream string, cursor map[string]any) (extract.Batch, error) {
	if resumed(cursor) {
		return s.finished(stream, cursor), nil
	}
	refs, err := s.ensureTeams(ctx)
	if err != nil {
		return extract.Batch{}, err
	}
	rows := make([]model.Row, 0, len(refs))
	for _, ref := range refs {
		rows = append(rows, s.teamRow(ref))
	}
	return extract.Batch{Stream: stream, Cursor: map[string]any{"done": true}, Rows: rows, Done: true}, nil
}

func (s *Source) fetchPlayers(ctx context.Context, stream string, cursor map[string]any) (extract.Batch, error) {
	ids, index, isResume, err := decodePlayersCursor(cursor)
	if err != nil {
		return extract.Batch{}, &extract.UnretryableError{
			Err: fmt.Errorf("sources: espn %q players cursor: %w", s.def.ID, err),
		}
	}
	if !isResume {
		if _, err := s.ensureTeams(ctx); err != nil {
			return extract.Batch{}, err
		}
		ids = s.refIDs()
		index = 0
	}
	if index >= len(ids) {
		return extract.Batch{Stream: stream, Cursor: playersCursor(ids, index), Done: true}, nil
	}

	teamID := ids[index]
	ref, ok := s.teamRefByID(teamID)
	if !ok {
		if _, err := s.ensureTeams(ctx); err != nil {
			return extract.Batch{}, err
		}
		ref, ok = s.teamRefByID(teamID)
	}
	if !ok {
		s.warnOnce("team id from cursor missing in listing", "team_id", teamID)
		return extract.Batch{Stream: stream, Cursor: playersCursor(ids, index+1)}, nil
	}
	roster, err := s.listRoster(ctx, ref.id)
	if err != nil {
		return extract.Batch{}, err
	}

	rows := playerRows(s.params.Code, ref, roster)
	next := index + 1
	if next >= len(ids) {
		return extract.Batch{Stream: stream, Cursor: playersCursor(ids, next), Rows: rows, Done: true}, nil
	}
	return extract.Batch{Stream: stream, Cursor: playersCursor(ids, next), Rows: rows}, nil
}

func (s *Source) fetchEvents(ctx context.Context, stream string, cursor map[string]any) (extract.Batch, error) {
	window, err := s.ensureWindow(ctx)
	if err != nil {
		return extract.Batch{}, err
	}
	walkEnd := s.walkEnd(window)
	day := window.start
	if cursor != nil {
		rawDate, ok := cursor["date"]
		if !ok || rawDate == nil {
			return extract.Batch{}, &extract.UnretryableError{
				Err: fmt.Errorf("sources: espn %q events cursor lacks a date entry", s.def.ID),
			}
		}
		day, err = parseDateCursor(rawDate)
		if err != nil {
			return extract.Batch{}, &extract.UnretryableError{
				Err: fmt.Errorf("sources: espn %q events cursor: %w", s.def.ID, err),
			}
		}
	}
	if day.After(walkEnd) {
		if walkEnd.Before(window.end) {
			return extract.Batch{Stream: stream, Cursor: s.refreshCursor(walkEnd, window), Done: true}, nil
		}
		return s.finished(stream, cursor), nil
	}

	if !s.hasTeams() {
		if _, err := s.ensureTeams(ctx); err != nil {
			return extract.Batch{}, err
		}
	}
	payload, err := s.listScoreboard(ctx, day)
	if err != nil {
		return extract.Batch{}, err
	}
	rows := s.eventRows(payload)

	nextDay := day.AddDate(0, 0, 1)
	nextCursor := map[string]any{"date": scoreboardDate(nextDay)}
	if nextDay.After(walkEnd) {
		if walkEnd.Before(window.end) {
			return extract.Batch{Stream: stream, Cursor: s.refreshCursor(walkEnd, window), Rows: rows, Done: true}, nil
		}
		return extract.Batch{Stream: stream, Cursor: nextCursor, Rows: rows, Done: true}, nil
	}
	return extract.Batch{Stream: stream, Cursor: nextCursor, Rows: rows}, nil
}

func (s *Source) walkEnd(window seasonWindow) time.Time {
	nowFn := s.now
	if nowFn == nil {
		nowFn = time.Now
	}
	today := dayFloor(nowFn())
	if window.end.Before(today) {
		return window.end
	}
	return today
}

func (s *Source) refreshCursor(walkEnd time.Time, window seasonWindow) map[string]any {
	parked := walkEnd.AddDate(0, 0, -refreshTailDays)
	if parked.Before(window.start) {
		parked = window.start
	}
	return map[string]any{"date": scoreboardDate(parked)}
}

func (s *Source) finished(stream string, cursor map[string]any) extract.Batch {
	if cursor == nil {
		cursor = map[string]any{"done": true}
	}
	return extract.Batch{Stream: stream, Cursor: cursor, Done: true}
}

func resumed(cursor map[string]any) bool {
	return cursor != nil
}

func (s *Source) listTeams(ctx context.Context) (teamsListPayload, error) {
	var payload teamsListPayload
	path := fmt.Sprintf("/apis/site/v2/sports/%s/%s/teams", s.params.Sport, s.params.League)
	if err := s.client.fetchSiteJSON(ctx, path, url.Values{}, &payload); err != nil {
		return teamsListPayload{}, err
	}
	return payload, nil
}

func (s *Source) leagueListingName(ctx context.Context) (string, error) {
	s.mu.Lock()
	name := s.leagueName
	s.mu.Unlock()
	if name != "" {
		return name, nil
	}
	payload, err := s.listTeams(ctx)
	if err != nil {
		return "", err
	}
	name = s.extractLeagueName(payload)
	if name == "" {
		return "", &extract.UnretryableError{
			Err: fmt.Errorf("sources: espn %q: team listing for %s/%s has no league name", s.def.ID, s.params.Sport, s.params.League),
		}
	}
	s.mu.Lock()
	if s.leagueName == "" {
		s.leagueName = name
	}
	s.mu.Unlock()
	return name, nil
}

func (s *Source) extractLeagueName(payload teamsListPayload) string {
	for _, sport := range payload.Sports {
		for _, league := range sport.Leagues {
			if name := strings.TrimSpace(league.Name); name != "" {
				return name
			}
		}
	}
	return ""
}

func (s *Source) ensureTeams(ctx context.Context) ([]teamRef, error) {
	s.mu.Lock()
	if len(s.teamRefs) > 0 {
		refs := append([]teamRef(nil), s.teamRefs...)
		s.mu.Unlock()
		return refs, nil
	}
	s.mu.Unlock()

	payload, err := s.listTeams(ctx)
	if err != nil {
		return nil, err
	}
	name := s.extractLeagueName(payload)
	if name != "" {
		s.mu.Lock()
		if s.leagueName == "" {
			s.leagueName = name
		}
		s.mu.Unlock()
	}

	teams := flattenTeams(payload)
	if len(teams) == 0 {
		return nil, &extract.UnretryableError{
			Err: fmt.Errorf("sources: espn %q: team listing for %s/%s returned no teams", s.def.ID, s.params.Sport, s.params.League),
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.teamRefs) > 0 {
		return append([]teamRef(nil), s.teamRefs...), nil
	}
	s.registerTeamsLocked(teams)
	return append([]teamRef(nil), s.teamRefs...), nil
}

func flattenTeams(payload teamsListPayload) []apiTeam {
	var teams []apiTeam
	for _, sport := range payload.Sports {
		for _, league := range sport.Leagues {
			for _, wrapped := range league.Teams {
				teams = append(teams, wrapped.Team)
			}
		}
	}
	return teams
}

func (s *Source) registerTeamsLocked(teams []apiTeam) {
	codes := map[string]string{}
	for _, apiTeam := range teams {
		id := strings.TrimSpace(apiTeam.ID)
		name := firstNonEmpty(apiTeam.DisplayName, apiTeam.Name, apiTeam.ShortDisplayName)
		if id == "" || name == "" {
			s.warnOnce("team listing entry without an id or a display name skipped", "team_id", id, "team_name", name)
			continue
		}
		short := firstNonEmpty(apiTeam.Abbreviation, apiTeam.ShortDisplayName)
		code := teamCode(short, name, id)
		if existing, collision := codes[code]; collision {
			s.warnOnce("duplicate team code, deriving a unique one", "code", code, "other_team", existing, "team_id", id)
			code = code + "-" + id
		}
		codes[code] = name
		ref := teamRef{id: id, code: code, short: short, name: name}
		s.teamRefs = append(s.teamRefs, ref)
		s.teamByID[id] = ref
		s.teamByName[name] = code
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *Source) listRoster(ctx context.Context, teamID string) ([]apiAthlete, error) {
	var payload rosterPayload
	path := fmt.Sprintf("/apis/site/v2/sports/%s/%s/teams/%s", s.params.Sport, s.params.League, teamID)
	if err := s.client.fetchSiteJSON(ctx, path, url.Values{"enable": []string{"roster"}}, &payload); err != nil {
		return nil, err
	}
	if payload.Team.ID != "" && payload.Team.ID != teamID {
		return nil, &extract.UnretryableError{
			Err: fmt.Errorf("sources: espn %q: roster for team %q answered with team %q", s.def.ID, teamID, payload.Team.ID),
		}
	}
	return payload.Team.Athletes, nil
}

func playerRows(leagueCode string, team teamRef, roster []apiAthlete) []model.Row {
	rows := make([]model.Row, 0, len(roster))
	codes := map[string]bool{}
	for _, entry := range roster {
		id := strings.TrimSpace(entry.ID)
		name := firstNonEmpty(entry.FullName, entry.DisplayName, entry.ShortName)
		if id == "" || name == "" {
			continue
		}
		code := slug(name)
		if code == "" || codes[code] {
			code = code + "-" + id
		}
		if codes[code] {
			continue
		}
		codes[code] = true
		rows = append(rows, model.Row{
			Kind: model.KindPlayer,
			Player: &model.Player{
				LeagueCode: leagueCode,
				Code:       code,
				FullName:   name,
				TeamCode:   team.code,
				Position:   positionName(entry.Position),
				Active:     true,
			},
		})
	}
	return rows
}

func (s *Source) ensureWindow(ctx context.Context) (seasonWindow, error) {
	s.mu.Lock()
	cached := s.window
	s.mu.Unlock()
	if cached != nil {
		return *cached, nil
	}
	if s.params.Season == "" {
		return seasonWindow{}, &extract.UnretryableError{
			Err: fmt.Errorf("sources: espn %q: the %s stream needs param season", s.def.ID, streamEvents),
		}
	}
	var payload seasonPayload
	path := fmt.Sprintf("/v2/sports/%s/leagues/%s/seasons/%s", s.params.Sport, s.params.League, s.params.Season)
	if err := s.client.fetchCoreJSON(ctx, path, &payload); err != nil {
		return seasonWindow{}, err
	}
	start, err := parseSeasonBoundary(payload.StartDate, "start_date")
	if err != nil {
		return seasonWindow{}, &extract.UnretryableError{Err: err}
	}
	end, err := parseSeasonBoundary(payload.EndDate, "end_date")
	if err != nil {
		return seasonWindow{}, &extract.UnretryableError{Err: err}
	}
	if end.Before(start) {
		return seasonWindow{}, &extract.UnretryableError{
			Err: fmt.Errorf("core season %s ends %s before it starts %s", s.params.Season, payload.EndDate, payload.StartDate),
		}
	}
	window := seasonWindow{start: start, end: end}
	s.mu.Lock()
	if s.window == nil {
		s.window = &window
	}
	s.mu.Unlock()
	return window, nil
}

func (s *Source) listScoreboard(ctx context.Context, day time.Time) (scoreboardPayload, error) {
	var payload scoreboardPayload
	path := fmt.Sprintf("/apis/site/v2/sports/%s/%s/scoreboard", s.params.Sport, s.params.League)
	if err := s.client.fetchSiteJSON(ctx, path, url.Values{"dates": []string{scoreboardDate(day)}}, &payload); err != nil {
		return scoreboardPayload{}, err
	}
	return payload, nil
}

func (s *Source) eventRows(payload scoreboardPayload) []model.Row {
	rows := make([]model.Row, 0, len(payload.Events))
	for _, event := range payload.Events {
		eventRows, skip := s.eventRow(event)
		if skip {
			continue
		}
		rows = append(rows, eventRows...)
	}
	return rows
}

func (s *Source) eventRow(event apiEvent) ([]model.Row, bool) {
	id := strings.TrimSpace(event.ID)
	if id == "" {
		s.warnOnce("event without an id skipped")
		return nil, true
	}
	startsAt, hasStart, err := parseStartsAt(event.Date)
	if err != nil {
		s.warnOnce("event with an unparsable date skipped", "event", id, "date", strings.TrimSpace(event.Date))
		return nil, true
	}
	if !hasStart {
		s.warnOnce("event without a date skipped", "event", id)
		return nil, true
	}
	competition, ok := firstCompetition(event.Competitions)
	if !ok {
		s.warnOnce("event without competitions skipped", "event", id)
		return nil, true
	}
	home, away, found := homeAwayCompetitors(competition.Competitors)
	if !found {
		s.warnOnce("event without a home and an away competitor skipped", "event", id)
		return nil, true
	}
	homeCode, rows := s.eventTeamCode(home.Team.ID, home.Team.Abbreviation, firstNonEmpty(home.Team.DisplayName, home.Team.Name))
	if homeCode == "" {
		s.warnOnce("event references an unresolvable home team, event skipped", "event", id)
		return nil, true
	}
	awayCode, extraRows := s.eventTeamCode(away.Team.ID, away.Team.Abbreviation, firstNonEmpty(away.Team.DisplayName, away.Team.Name))
	rows = append(rows, extraRows...)
	if awayCode == "" {
		s.warnOnce("event references an unresolvable away team, event skipped", "event", id)
		return nil, true
	}
	homeScore, err := parseScore(home.Score)
	if err != nil {
		s.warnOnce("event with an unparsable home score skipped", "event", id, "score", strings.TrimSpace(home.Score))
		return nil, true
	}
	awayScore, err := parseScore(away.Score)
	if err != nil {
		s.warnOnce("event with an unparsable away score skipped", "event", id, "score", strings.TrimSpace(away.Score))
		return nil, true
	}
	statusType := event.Status.Type
	status, mapped := mapStatus(statusType.State, statusType.Name, homeScore != nil || awayScore != nil)
	if !mapped {
		s.warnOnce("unknown event status, fell back by scores", "event", id, "state", strings.TrimSpace(statusType.State), "status_name", strings.TrimSpace(statusType.Name), "status_fallback", status)
	}
	venue := firstNonEmpty(competition.Venue.FullName, event.Venue.FullName)
	rows = append(rows, model.Row{
		Kind: model.KindGame,
		Game: &model.Game{
			LeagueCode:   s.params.Code,
			GameCode:     id,
			Season:       seasonValue(competition.Season, s.params.Season),
			StartsAt:     startsAt,
			Status:       status,
			HomeTeamCode: homeCode,
			AwayTeamCode: awayCode,
			HomeScore:    homeScore,
			AwayScore:    awayScore,
			Venue:        venue,
		},
	})
	return rows, false
}

func firstCompetition(competitions []apiCompetition) (apiCompetition, bool) {
	for _, competition := range competitions {
		return competition, true
	}
	return apiCompetition{}, false
}

func homeAwayCompetitors(competitors []apiCompetitor) (apiCompetitor, apiCompetitor, bool) {
	var home, away apiCompetitor
	var haveHome, haveAway bool
	for _, competitor := range competitors {
		switch strings.ToLower(strings.TrimSpace(competitor.HomeAway)) {
		case "home":
			home, haveHome = competitor, true
		case "away":
			away, haveAway = competitor, true
		}
	}
	return home, away, haveHome && haveAway
}

func seasonValue(eventSeason *int, paramsSeason string) string {
	if eventSeason != nil && *eventSeason > 0 {
		return strconv.Itoa(*eventSeason)
	}
	return paramsSeason
}

func (s *Source) eventTeamCode(id, abbreviation, name string) (string, []model.Row) {
	if id == "" && name == "" {
		return "", nil
	}
	if id != "" {
		s.mu.Lock()
		ref, ok := s.teamByID[id]
		s.mu.Unlock()
		if ok {
			return ref.code, nil
		}
	}
	if name != "" {
		if code, ok := s.teamNameCode(name); ok {
			return code, nil
		}
	}
	code := teamCode(abbreviation, name, id)
	s.warnOnce("event team missing from the listing, derived a fallback team row", "team", name, "team_id", id, "code", code)
	derived := teamRef{id: id, code: code, short: abbreviation, name: name}
	s.mu.Lock()
	if id != "" {
		s.teamByID[id] = derived
	}
	if name != "" {
		s.teamByName[name] = code
	}
	s.mu.Unlock()
	return code, []model.Row{{
		Kind: model.KindTeam,
		Team: &model.Team{LeagueCode: s.params.Code, Code: code, Name: name},
	}}
}

func (s *Source) teamRow(ref teamRef) model.Row {
	return model.Row{
		Kind: model.KindTeam,
		Team: &model.Team{
			LeagueCode:   s.params.Code,
			Code:         ref.code,
			Name:         ref.name,
			Abbreviation: ref.short,
		},
	}
}

func (s *Source) hasTeams() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.teamRefs) > 0
}

func (s *Source) teamNameCode(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	code, ok := s.teamByName[name]
	return code, ok
}

func (s *Source) teamRefByID(teamID string) (teamRef, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, ok := s.teamByID[teamID]
	return ref, ok
}

func (s *Source) refIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.teamRefs))
	for _, ref := range s.teamRefs {
		ids = append(ids, ref.id)
	}
	return ids
}

func (s *Source) warnOnce(message string, args ...any) {
	s.mu.Lock()
	_, seen := s.warned[message]
	s.warned[message] = true
	s.mu.Unlock()
	if seen {
		return
	}
	s.logger.Warn(message, args...)
}

func decodePlayersCursor(cursor map[string]any) ([]string, int, bool, error) {
	if cursor == nil {
		return nil, 0, false, nil
	}
	raw, ok := cursor["team_ids"]
	if !ok || raw == nil {
		return nil, 0, false, nil
	}
	ids, err := decodeTeamIDs(raw)
	if err != nil {
		return nil, 0, false, err
	}
	index := 0
	if rawIndex, ok := cursor["idx"]; ok && rawIndex != nil {
		switch value := rawIndex.(type) {
		case int:
			index = value
		case float64:
			index = int(value)
		default:
			return nil, 0, false, fmt.Errorf("idx %T is not a number", rawIndex)
		}
	}
	if index < 0 {
		return nil, 0, false, fmt.Errorf("idx %d must not be negative", index)
	}
	return ids, index, true, nil
}

func decodeTeamIDs(raw any) ([]string, error) {
	switch list := raw.(type) {
	case []string:
		return list, nil
	case []any:
		ids := make([]string, 0, len(list))
		for _, entry := range list {
			id, ok := entry.(string)
			if !ok {
				if number, ok := entry.(float64); ok {
					ids = append(ids, strconv.FormatFloat(number, 'f', -1, 64))
					continue
				}
				return nil, fmt.Errorf("team_ids entry %T is not a string", entry)
			}
			ids = append(ids, id)
		}
		return ids, nil
	default:
		return nil, fmt.Errorf("team_ids %T is not a list", raw)
	}
}

func playersCursor(ids []string, index int) map[string]any {
	return map[string]any{"team_ids": ids, "idx": index}
}
