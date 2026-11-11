package tsdb

import (
	"log/slog"
	"os"
	"testing"

	"sports-analytics/ingestor/extract"
	"sports-analytics/ingestor/model"
)

func TestLiveEPLSeason(t *testing.T) {
	if os.Getenv("TSDB_LIVE") != "1" {
		t.Skip("opt-in: run with TSDB_LIVE=1 to hit the real TheSportsDB API (~13 paced calls)")
	}

	def := extract.SourceDef{
		ID:      "tsdb-live",
		Type:    SourceType,
		Streams: []string{"leagues", "teams", "players", "events"},
		Params: map[string]string{
			"league_id": os.Getenv("INGESTOR_LIVE_LEAGUE_ID"),
			"code":      os.Getenv("INGESTOR_LIVE_LEAGUE_CODE"),
			"season":    os.Getenv("INGESTOR_LIVE_SEASON"),
		},
	}
	if def.Params["league_id"] == "" {
		def.Params["league_id"] = "4328"
	}
	if def.Params["code"] == "" {
		def.Params["code"] = "EPL"
	}
	if def.Params["season"] == "" {
		def.Params["season"] = "2023-2024"
	}

	options := Options{
		APIKey:  os.Getenv("INGESTOR_THESPORTSDB_API_KEY"),
		BaseURL: os.Getenv("INGESTOR_THESPORTSDB_BASE_URL"),
	}
	factory := Factory(options)
	source, err := factory(def)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	seenTeams := map[string]bool{}
	totals := map[string]int{}
	for _, stream := range def.Streams {
		batches := walkStream(t, source, stream)
		for _, batch := range batches {
			for _, row := range batch.Rows {
				switch row.Kind {
				case model.KindLeague:
					totals["leagues"]++
					league := row.League
					if league.Code == "" || league.Name == "" || league.Sport == "" {
						t.Errorf("live league row incomplete: %+v", league)
					}
					slog.Info("live league", "code", league.Code, "name", league.Name, "sport", league.Sport)
				case model.KindTeam:
					totals["teams"]++
					seenTeams[row.Team.Code] = true
				case model.KindPlayer:
					totals["players"]++
				case model.KindGame:
					totals["games"]++
					game := row.Game
					if game.StartsAt.IsZero() {
						t.Errorf("game %s has zero starts_at", game.GameCode)
					}
					if pending, ok := gameStatuses[game.Status]; !ok || !pending {
						t.Errorf("game %s status %q is not a valid db status", game.GameCode, game.Status)
					}
					if !seenTeams[game.HomeTeamCode] || !seenTeams[game.AwayTeamCode] {
						t.Errorf("game %s references unloaded teams %s/%s (stream order broken?)", game.GameCode, game.HomeTeamCode, game.AwayTeamCode)
					}
				}
			}
		}
		slog.Info("live stream finished", "stream", stream, "rows", totals)
	}
	if totals["leagues"] != 1 {
		t.Errorf("leagues = %d, want 1", totals["leagues"])
	}
	if totals["teams"] < 5 {
		t.Errorf("teams = %d, want at least 5 (free-key truncation)", totals["teams"])
	}
	if totals["games"] < 10 {
		t.Errorf("games = %d, want at least 10 (free-key truncation)", totals["games"])
	}
	t.Logf("live totals: %+v", totals)
}

var gameStatuses = map[string]bool{"scheduled": true, "live": true, "final": true, "postponed": true, "canceled": true}
