package tsdb

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"sports-analytics/ingestor/extract"
	"sports-analytics/ingestor/model"
)

const (
	streamLeagues = "leagues"
	streamTeams   = "teams"
	streamPlayers = "players"
	streamEvents  = "events"
)

type Params struct {
	LeagueID string
	Code     string
	Season   string
}

func parseParams(def extract.SourceDef) (Params, error) {
	params := Params{
		LeagueID: strings.TrimSpace(def.Params["league_id"]),
		Code:     strings.TrimSpace(def.Params["code"]),
		Season:   strings.TrimSpace(def.Params["season"]),
	}
	if params.LeagueID == "" {
		return Params{}, fmt.Errorf("param league_id is required")
	}
	if params.Code == "" {
		return Params{}, fmt.Errorf("param code is required")
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

type leagueInfo struct {
	name  string
	sport string
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
}

func Factory(options Options) extract.Factory {
	return func(def extract.SourceDef) (extract.Source, error) {
		params, err := parseParams(def)
		if err != nil {
			return nil, fmt.Errorf("sources: tsdb %q: %w", def.ID, err)
		}
		return &Source{
			def:        def,
			params:     params,
			client:     newClient(options),
			logger:     slog.Default(),
			teamByID:   map[string]teamRef{},
			teamByName: map[string]string{},
			warned:     map[string]bool{},
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
			Err: fmt.Errorf("sources: tsdb %q has no stream %q", s.def.ID, stream),
		}
	}
}

func (s *Source) fetchLeagues(ctx context.Context, stream string, cursor map[string]any) (extract.Batch, error) {
	if resumed(cursor) {
		return s.finished(stream, cursor), nil
	}
	league, err := s.lookupLeague(ctx)
	if err != nil {
		return extract.Batch{}, err
	}
	rows := []model.Row{{
		Kind:   model.KindLeague,
		League: &model.League{Code: s.params.Code, Name: league.name, Sport: league.sport},
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
			Err: fmt.Errorf("sources: tsdb %q players cursor: %w", s.def.ID, err),
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
	ref, ok := s.teamRef(teamID)
	if !ok {
		if _, err := s.ensureTeams(ctx); err != nil {
			return extract.Batch{}, err
		}
		ref, ok = s.teamRef(teamID)
	}
	if !ok {
		s.warnOnce("team id from cursor missing in listing", "team_id", teamID)
		return extract.Batch{Stream: stream, Cursor: playersCursor(ids, index+1)}, nil
	}
	roster, err := s.listPlayers(ctx, ref.id)
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
	if resumed(cursor) {
		return s.finished(stream, cursor), nil
	}
	if !s.hasTeams() {
		if _, err := s.ensureTeams(ctx); err != nil {
			return extract.Batch{}, err
		}
	}
	events, err := s.listEvents(ctx)
	if err != nil {
		return extract.Batch{}, err
	}
	rows := make([]model.Row, 0, len(events))
	for _, event := range events {
		eventModelRows, err := s.eventRows(event)
		if err != nil {
			return extract.Batch{}, err
		}
		rows = append(rows, eventModelRows...)
	}
	return extract.Batch{Stream: stream, Cursor: map[string]any{"done": true}, Rows: rows, Done: true}, nil
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

func (s *Source) ensureLeague(ctx context.Context) (string, error) {
	s.mu.Lock()
	name := s.leagueName
	s.mu.Unlock()
	if name != "" {
		return name, nil
	}
	league, err := s.lookupLeague(ctx)
	if err != nil {
		return "", err
	}
	return league.name, nil
}

func (s *Source) ensureTeams(ctx context.Context) ([]teamRef, error) {
	s.mu.Lock()
	if len(s.teamRefs) > 0 {
		refs := append([]teamRef(nil), s.teamRefs...)
		s.mu.Unlock()
		return refs, nil
	}
	s.mu.Unlock()

	leagueName, err := s.ensureLeague(ctx)
	if err != nil {
		return nil, err
	}
	teams, err := s.listTeams(ctx, leagueName)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.teamRefs) > 0 {
		return append([]teamRef(nil), s.teamRefs...), nil
	}
	s.registerTeamsLocked(teams)
	return append([]teamRef(nil), s.teamRefs...), nil
}

func (s *Source) registerTeamsLocked(teams []apiTeam) {
	codes := map[string]string{}
	for _, apiTeam := range teams {
		name := strings.TrimSpace(apiTeam.Name)
		id := strings.TrimSpace(apiTeam.ID)
		if name == "" || id == "" {
			continue
		}
		code := teamCode(apiTeam.Short, name, id)
		if existing, collision := codes[code]; collision {
			s.warnOnce("duplicate team code, deriving a unique one", "code", code, "other_team", existing, "team_id", id)
			code = code + "-" + id
		}
		codes[code] = name
		ref := teamRef{id: id, code: code, short: strings.TrimSpace(apiTeam.Short), name: name}
		s.teamRefs = append(s.teamRefs, ref)
		s.teamByID[id] = ref
		s.teamByName[name] = code
	}
}

func (s *Source) lookupLeague(ctx context.Context) (leagueInfo, error) {
	var payload leaguesPayload
	if err := s.client.fetchJSON(ctx, "lookupleague.php", url.Values{"id": []string{s.params.LeagueID}}, &payload); err != nil {
		return leagueInfo{}, err
	}
	for _, entry := range payload.Leagues {
		if strings.TrimSpace(entry.ID) == s.params.LeagueID {
			info := leagueInfo{
				name:  strings.TrimSpace(entry.Name),
				sport: sport(entry.Sport),
			}
			s.mu.Lock()
			s.leagueName = info.name
			s.mu.Unlock()
			return info, nil
		}
	}
	return leagueInfo{}, &extract.UnretryableError{
		Err: fmt.Errorf("sources: tsdb %q: lookupleague id %q returned no matching league", s.def.ID, s.params.LeagueID),
	}
}

func (s *Source) listTeams(ctx context.Context, leagueName string) ([]apiTeam, error) {
	var payload teamsPayload
	if err := s.client.fetchJSON(ctx, "search_all_teams.php", url.Values{"l": []string{leagueName}}, &payload); err != nil {
		return nil, err
	}
	if len(payload.Teams) == 0 {
		return nil, &extract.UnretryableError{
			Err: fmt.Errorf("sources: tsdb %q: search_all_teams league %q returned no teams", s.def.ID, leagueName),
		}
	}
	return payload.Teams, nil
}

func (s *Source) listPlayers(ctx context.Context, teamID string) ([]apiPlayer, error) {
	var payload playersPayload
	if err := s.client.fetchJSON(ctx, "lookup_all_players.php", url.Values{"id": []string{teamID}}, &payload); err != nil {
		return nil, err
	}
	return payload.Player, nil
}

func (s *Source) listEvents(ctx context.Context) ([]apiEvent, error) {
	var payload eventsPayload
	if err := s.client.fetchJSON(ctx, "eventsseason.php", url.Values{
		"id": []string{s.params.LeagueID},
		"s":  []string{s.params.Season},
	}, &payload); err != nil {
		return nil, err
	}
	if len(payload.Events) == 0 {
		return nil, &extract.UnretryableError{
			Err: fmt.Errorf("sources: tsdb %q: eventsseason league %q season %q returned no events", s.def.ID, s.params.LeagueID, s.params.Season),
		}
	}
	return payload.Events, nil
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

func (s *Source) eventRows(event apiEvent) ([]model.Row, error) {
	id := strings.TrimSpace(event.ID)
	if id == "" {
		s.warnOnce("event without idEvent skipped", "name", strings.TrimSpace(event.Name))
		return nil, nil
	}
	startsAt, hasStart, err := parseStartsAt(event.Timestamp, event.Date, event.Time)
	if err != nil {
		return nil, fmt.Errorf("sources: tsdb %q event %s: %w", s.def.ID, id, err)
	}
	if !hasStart {
		s.warnOnce("event without kickoff time skipped", "event", id)
		return nil, nil
	}
	homeScore, err := parseScore(event.HomeScore)
	if err != nil {
		return nil, fmt.Errorf("sources: tsdb %q event %s: %w", s.def.ID, id, err)
	}
	awayScore, err := parseScore(event.AwayScore)
	if err != nil {
		return nil, fmt.Errorf("sources: tsdb %q event %s: %w", s.def.ID, id, err)
	}

	rows := make([]model.Row, 0, 3)
	homeCode, err := s.eventTeamCode(event.Home, &rows)
	if err != nil {
		return nil, err
	}
	awayCode, err := s.eventTeamCode(event.Away, &rows)
	if err != nil {
		return nil, err
	}
	if homeCode == "" || awayCode == "" {
		return nil, nil
	}

	hasScore := homeScore != nil || awayScore != nil
	status, mapped := mapStatus(event.Status, event.Postponed, hasScore)
	if !mapped {
		s.warnOnce("unknown event status, fell back by scores", "event", id, "status", strings.TrimSpace(event.Status), "status_fallback", status)
	}

	rows = append(rows, model.Row{
		Kind: model.KindGame,
		Game: &model.Game{
			LeagueCode:   s.params.Code,
			GameCode:     id,
			Season:       seasonValue(event.Season, s.params.Season),
			StartsAt:     startsAt.UTC(),
			Status:       status,
			HomeTeamCode: homeCode,
			AwayTeamCode: awayCode,
			HomeScore:    homeScore,
			AwayScore:    awayScore,
			Venue:        strings.TrimSpace(event.Venue),
		},
	})
	return rows, nil
}

func seasonValue(eventSeason, paramsSeason string) string {
	season := strings.TrimSpace(eventSeason)
	if season != "" {
		return season
	}
	return paramsSeason
}

func (s *Source) eventTeamCode(name string, rows *[]model.Row) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		s.warnOnce("event references an empty team name, event skipped")
		return "", nil
	}
	if code, ok := s.teamNameCode(trimmed); ok {
		return code, nil
	}
	code := slug(trimmed)
	if code == "" {
		code = "tsdb-unknown"
	}
	s.warnOnce("event team missing from the truncated listing, derived a fallback team row", "team", trimmed, "code", code)
	s.mu.Lock()
	if _, exists := s.teamByName[trimmed]; !exists {
		s.teamByName[trimmed] = code
	}
	s.mu.Unlock()
	*rows = append(*rows, model.Row{
		Kind: model.KindTeam,
		Team: &model.Team{LeagueCode: s.params.Code, Code: code, Name: trimmed},
	})
	return code, nil
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

func (s *Source) teamRef(teamID string) (teamRef, bool) {
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

func teamCode(short, name, id string) string {
	if trimmed := strings.TrimSpace(short); trimmed != "" {
		return trimmed
	}
	if code := slug(name); code != "" {
		return code
	}
	return "tsdb-" + id
}

func playerRows(leagueCode string, team teamRef, roster []apiPlayer) []model.Row {
	rows := make([]model.Row, 0, len(roster))
	codes := map[string]bool{}
	for _, entry := range roster {
		name := strings.TrimSpace(entry.Name)
		position := strings.TrimSpace(entry.Position)
		if name == "" || isCoach(position) {
			continue
		}
		code := slug(name)
		if code == "" || codes[code] {
			code = code + "-" + strings.TrimSpace(entry.ID)
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
				Position:   position,
				Active:     true,
			},
		})
	}
	return rows
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
