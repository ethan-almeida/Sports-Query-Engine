package espn

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"sports-analytics/ingestor/extract"
	"sports-analytics/ingestor/model"
)

func TestLiveLeagueSeason(t *testing.T) {
	if os.Getenv("ESPN_LIVE") != "1" {
		t.Skip("opt-in: run with ESPN_LIVE=1 to hit the real ESPN API (~300 paced calls for a full soccer season)")
	}

	def := extract.SourceDef{
		ID:      "espn-live",
		Type:    SourceType,
		Streams: []string{"leagues", "teams", "players", "events"},
		Params: map[string]string{
			"sport":  os.Getenv("INGESTOR_LIVE_ESPN_SPORT"),
			"league": os.Getenv("INGESTOR_LIVE_ESPN_LEAGUE"),
			"code":   os.Getenv("INGESTOR_LIVE_LEAGUE_CODE"),
			"season": os.Getenv("INGESTOR_LIVE_SEASON"),
		},
	}
	if def.Params["sport"] == "" {
		def.Params["sport"] = supportedSoccer
	}
	if def.Params["league"] == "" {
		def.Params["league"] = "eng.1"
	}
	if def.Params["code"] == "" {
		def.Params["code"] = "EPL"
	}
	if def.Params["season"] == "" {
		def.Params["season"] = "2023"
	}

	options := Options{
		SiteBaseURL: os.Getenv("INGESTOR_ESPN_BASE_URL"),
		CoreBaseURL: os.Getenv("INGESTOR_ESPN_CORE_BASE_URL"),
	}
	factory := Factory(options)
	source, err := factory(def)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	seenTeams := map[string]bool{}
	totals := map[string]int{}
	for _, stream := range def.Streams {
		batches := liveWalk(t, source, stream)
		for _, batch := range batches {
			for _, row := range batch.Rows {
				switch row.Kind {
				case model.KindLeague:
					totals["leagues"]++
					league := row.League
					if league.Code == "" || league.Name == "" || league.Sport == "" {
						t.Errorf("live league row incomplete: %+v", league)
					}
					if league.Sport != def.Params["sport"] {
						t.Errorf("live league sport = %q, want %q", league.Sport, def.Params["sport"])
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
	minimumTeams := 18
	if def.Params["sport"] == supportedBasketball {
		minimumTeams = 25
	}
	if totals["teams"] < minimumTeams {
		t.Errorf("teams = %d, want at least %d", totals["teams"], minimumTeams)
	}
	minimumGames := 150
	if totals["games"] < minimumGames {
		t.Errorf("games = %d, want at least %d", totals["games"], minimumGames)
	}
	t.Logf("live totals: %+v", totals)
}

var gameStatuses = map[string]bool{"scheduled": true, "live": true, "final": true, "postponed": true, "canceled": true}

func liveWalk(t *testing.T, source extract.Source, stream string) []extract.Batch {
	t.Helper()
	var batches []extract.Batch
	cursor := map[string]any(nil)
	for page := 0; page < 500; page++ {
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
	t.Fatalf("stream %s failed to terminate within 500 pages", stream)
	return nil
}
