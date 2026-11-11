package sources

import (
	"context"
	"fmt"
	"time"

	"sports-analytics/ingestor/extract"
	"sports-analytics/ingestor/model"
	"sports-analytics/ingestor/sources/espn"
	"sports-analytics/ingestor/sources/tsdb"
)

const syntheticSourceType = "synthetic"

func Factory() extract.Factory {
	return func(def extract.SourceDef) (extract.Source, error) {
		if len(def.Streams) == 0 {
			return nil, fmt.Errorf("sources: synthetic source %q needs at least one stream", def.ID)
		}
		return &Synthetic{def: def}, nil
	}
}

type Registry map[string]extract.Factory

type Options struct {
	TheSportsDBAPIKey  string
	TheSportsDBBaseURL string
	ESPNBaseURL        string
	ESPNCoreBaseURL    string
}

func DefaultRegistry(options Options) Registry {
	tsdbFactory := tsdb.Factory(tsdb.Options{APIKey: options.TheSportsDBAPIKey, BaseURL: options.TheSportsDBBaseURL})
	espnFactory := espn.Factory(espn.Options{SiteBaseURL: options.ESPNBaseURL, CoreBaseURL: options.ESPNCoreBaseURL})
	return map[string]extract.Factory{
		syntheticSourceType: Factory(),
		tsdb.SourceType:     tsdbFactory,
		espn.SourceType:     espnFactory,
	}
}

type Synthetic struct {
	def extract.SourceDef
}

func (s *Synthetic) Def() extract.SourceDef { return s.def }

func (s *Synthetic) Fetch(ctx context.Context, stream string, cursor map[string]any) (extract.Batch, error) {
	if !containsString(s.def.Streams, stream) {
		return extract.Batch{}, &extract.UnretryableError{
			Err: fmt.Errorf("sources: synthetic source %q has no stream %q", s.def.ID, stream),
		}
	}
	page, err := decodePage(cursor)
	if err != nil {
		return extract.Batch{}, &extract.UnretryableError{Err: fmt.Errorf("sources: cursor for %s/%s: %w", s.def.ID, stream, err)}
	}

	rows, done, nextPage, err := synthesizePage(page)
	if err != nil {
		return extract.Batch{}, &extract.UnretryableError{Err: err}
	}
	return extract.Batch{Stream: stream, Cursor: map[string]any{"page": nextPage}, Rows: rows, Done: done}, nil
}

func decodePage(cursor map[string]any) (int, error) {
	if cursor == nil {
		return 0, nil
	}
	raw, ok := cursor["page"]
	if !ok || raw == nil {
		return 0, nil
	}
	switch value := raw.(type) {
	case int:
		if value < 0 {
			return 0, fmt.Errorf("page %d must not be negative", value)
		}
		return value, nil
	case float64:
		if value < 0 || value != float64(int(value)) {
			return 0, fmt.Errorf("page %v must be a whole non-negative number", value)
		}
		return int(value), nil
	default:
		return 0, fmt.Errorf("page %T is not a number", raw)
	}
}

func synthesizePage(page int) ([]model.Row, bool, int, error) {
	switch page {
	case 0:
		return append(leagueRows(), teamRows()...), false, 1, nil
	case 1:
		return playerRows(), false, 2, nil
	case 2:
		return gameRows(), false, 3, nil
	case 3:
		return statRows(), true, 4, nil
	case 4:
		return nil, true, 4, nil
	default:
		return nil, false, 0, fmt.Errorf("page %d is out of range for the synthetic dataset", page)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, candidate := range haystack {
		if candidate == needle {
			return true
		}
	}
	return false
}

func leagueRows() []model.Row {
	return []model.Row{
		{Kind: model.KindLeague, League: &model.League{Code: "NBA", Name: "National Basketball Association", Sport: "basketball"}},
		{Kind: model.KindLeague, League: &model.League{Code: "LOL", Name: "League of Legends Pro League", Sport: "esports"}},
		{Kind: model.KindLeague, League: &model.League{Code: "SYN", Name: "Synthetic Demo League", Sport: "synthetic"}},
	}
}

func teamRows() []model.Row {
	return []model.Row{
		{Kind: model.KindTeam, Team: &model.Team{LeagueCode: "NBA", Code: "LAL", Name: "Los Angeles Lakers", Abbreviation: "LAL", Conference: "Western", Division: "Pacific"}},
		{Kind: model.KindTeam, Team: &model.Team{LeagueCode: "NBA", Code: "BOS", Name: "Boston Celtics", Abbreviation: "BOS", Conference: "Eastern", Division: "Atlantic"}},
		{Kind: model.KindTeam, Team: &model.Team{LeagueCode: "NBA", Code: "CHI", Name: "Chicago Bulls", Abbreviation: "CHI", Conference: "Eastern", Division: "Central"}},
		{Kind: model.KindTeam, Team: &model.Team{LeagueCode: "LOL", Code: "T1", Name: "T1", Abbreviation: "T1"}},
		{Kind: model.KindTeam, Team: &model.Team{LeagueCode: "LOL", Code: "G2", Name: "G2 Esports", Abbreviation: "G2"}},
		{Kind: model.KindTeam, Team: &model.Team{LeagueCode: "SYN", Code: "ALPHA", Name: "Team Alpha", Abbreviation: "ALP"}},
		{Kind: model.KindTeam, Team: &model.Team{LeagueCode: "SYN", Code: "BETA", Name: "Team Beta", Abbreviation: "BTA"}},
	}
}

func playerRows() []model.Row {
	return []model.Row{
		{Kind: model.KindPlayer, Player: &model.Player{LeagueCode: "NBA", Code: "curry", FullName: "Stephen Curry", TeamCode: "LAL", Position: "G", Active: true}},
		{Kind: model.KindPlayer, Player: &model.Player{LeagueCode: "NBA", Code: "lebron", FullName: "LeBron James", TeamCode: "LAL", Position: "F", Active: true}},
		{Kind: model.KindPlayer, Player: &model.Player{LeagueCode: "NBA", Code: "tatum", FullName: "Jayson Tatum", TeamCode: "BOS", Position: "F", Active: true}},
		{Kind: model.KindPlayer, Player: &model.Player{LeagueCode: "NBA", Code: "lavine", FullName: "Zach LaVine", TeamCode: "CHI", Position: "G", Active: true}},
		{Kind: model.KindPlayer, Player: &model.Player{LeagueCode: "LOL", Code: "faker", FullName: "Faker", TeamCode: "T1", Position: "MID", Active: true}},
		{Kind: model.KindPlayer, Player: &model.Player{LeagueCode: "LOL", Code: "caps", FullName: "Caps", TeamCode: "G2", Position: "MID", Active: true}},
		{Kind: model.KindPlayer, Player: &model.Player{LeagueCode: "SYN", Code: "alpha", FullName: "Alpha One", TeamCode: "ALPHA", Position: "ST", Active: true}},
		{Kind: model.KindPlayer, Player: &model.Player{LeagueCode: "SYN", Code: "beta", FullName: "Beta Two", TeamCode: "BETA", Position: "ST", Active: true}},
	}
}

func at(day int, hour int) time.Time {
	return time.Date(2025, time.October, day, hour, 0, 0, 0, time.UTC)
}

func intPointer(value int) *int {
	return &value
}

func gameRows() []model.Row {
	return []model.Row{
		{Kind: model.KindGame, Game: &model.Game{LeagueCode: "NBA", GameCode: "NBA-001", Season: "2025-26", StartsAt: at(20, 23), Status: "final", HomeTeamCode: "BOS", AwayTeamCode: "LAL", HomeScore: intPointer(108), AwayScore: intPointer(112), Venue: "TD Garden"}},
		{Kind: model.KindGame, Game: &model.Game{LeagueCode: "NBA", GameCode: "NBA-002", Season: "2025-26", StartsAt: at(21, 23), Status: "final", HomeTeamCode: "LAL", AwayTeamCode: "CHI", HomeScore: intPointer(118), AwayScore: intPointer(104), Venue: "Crypto.com Arena"}},
		{Kind: model.KindGame, Game: &model.Game{LeagueCode: "NBA", GameCode: "NBA-003", Season: "2025-26", StartsAt: at(22, 23), Status: "final", HomeTeamCode: "CHI", AwayTeamCode: "BOS", HomeScore: intPointer(102), AwayScore: intPointer(110), Venue: "United Center"}},
		{Kind: model.KindGame, Game: &model.Game{LeagueCode: "NBA", GameCode: "NBA-004", Season: "2025-26", StartsAt: at(26, 23), Status: "scheduled", HomeTeamCode: "LAL", AwayTeamCode: "BOS", Venue: "Crypto.com Arena"}},
		{Kind: model.KindGame, Game: &model.Game{LeagueCode: "LOL", GameCode: "LOL-001", Season: "Worlds-2025", StartsAt: at(20, 18), Status: "final", HomeTeamCode: "T1", AwayTeamCode: "G2", HomeScore: intPointer(3), AwayScore: intPointer(1), Venue: "Stage A"}},
		{Kind: model.KindGame, Game: &model.Game{LeagueCode: "LOL", GameCode: "LOL-002", Season: "Worlds-2025", StartsAt: at(21, 18), Status: "final", HomeTeamCode: "G2", AwayTeamCode: "T1", HomeScore: intPointer(2), AwayScore: intPointer(3), Venue: "Stage A"}},
		{Kind: model.KindGame, Game: &model.Game{LeagueCode: "SYN", GameCode: "SYN-001", Season: "2025", StartsAt: at(23, 19), Status: "final", HomeTeamCode: "ALPHA", AwayTeamCode: "BETA", HomeScore: intPointer(88), AwayScore: intPointer(77), Venue: "Synthetic Dome"}},
		{Kind: model.KindGame, Game: &model.Game{LeagueCode: "SYN", GameCode: "SYN-002", Season: "2025", StartsAt: at(24, 19), Status: "live", HomeTeamCode: "BETA", AwayTeamCode: "ALPHA", HomeScore: intPointer(92), AwayScore: intPointer(90), Venue: "Synthetic Dome"}},
	}
}

func stat(league, game, player, name string, value float64) model.Row {
	return model.Row{Kind: model.KindStat, Stat: &model.Stat{
		LeagueCode: league, GameCode: game, PlayerCode: player, StatName: name, StatValue: value,
	}}
}

func statRows() []model.Row {
	return []model.Row{
		stat("NBA", "NBA-001", "curry", "points", 34),
		stat("NBA", "NBA-001", "curry", "assists", 7),
		stat("NBA", "NBA-001", "tatum", "points", 28),
		stat("NBA", "NBA-001", "tatum", "rebounds", 9),
		stat("NBA", "NBA-001", "lebron", "points", 22),
		stat("NBA", "NBA-002", "lebron", "points", 30),
		stat("NBA", "NBA-002", "lebron", "rebounds", 8),
		stat("NBA", "NBA-002", "curry", "points", 27),
		stat("NBA", "NBA-002", "lavine", "points", 19),
		stat("NBA", "NBA-003", "tatum", "points", 31),
		stat("NBA", "NBA-003", "tatum", "assists", 5),
		stat("NBA", "NBA-003", "tatum", "steals", 2),
		stat("NBA", "NBA-003", "lavine", "points", 24),
		stat("NBA", "NBA-003", "lavine", "rebounds", 6),
		stat("NBA", "NBA-003", "lavine", "assists", 4),
		stat("LOL", "LOL-001", "faker", "kills", 12),
		stat("LOL", "LOL-001", "faker", "assists", 3),
		stat("LOL", "LOL-001", "caps", "kills", 8),
		stat("LOL", "LOL-001", "caps", "assists", 5),
		stat("LOL", "LOL-002", "faker", "kills", 9),
		stat("LOL", "LOL-002", "caps", "kills", 11),
		stat("SYN", "SYN-001", "alpha", "points", 41),
		stat("SYN", "SYN-001", "beta", "points", 33),
		stat("SYN", "SYN-002", "alpha", "points", 38),
		stat("SYN", "SYN-002", "beta", "points", 40),
	}
}
