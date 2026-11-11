package sources

import (
	"context"
	"reflect"
	"testing"

	"sports-analytics/ingestor/extract"
	"sports-analytics/ingestor/model"
	"sports-analytics/ingestor/sources/espn"
	"sports-analytics/ingestor/sources/tsdb"
)

func walkAll(t *testing.T, source extract.Source) ([]model.Row, int) {
	t.Helper()
	var all []model.Row
	cursor := map[string]any(nil)
	pages := 0
	for {
		batch, err := source.Fetch(context.Background(), "snapshot", cursor)
		if err != nil {
			t.Fatalf("fetch page %v: %v", cursor, err)
		}
		all = append(all, batch.Rows...)
		pages++
		if batch.Done {
			terminalPage, ok := batch.Cursor["page"].(int)
			if !ok {
				t.Fatalf("terminal cursor missing integer page: %v", batch.Cursor)
			}
			tail, err := source.Fetch(context.Background(), "snapshot", batch.Cursor)
			if err != nil {
				t.Fatalf("fetch terminal cursor %v: %v", batch.Cursor, err)
			}
			if len(tail.Rows) != 0 || !tail.Done {
				t.Fatalf("terminal cursor must yield no rows, got %d rows done=%v", len(tail.Rows), tail.Done)
			}
			return all, terminalPage
		}
		cursor = batch.Cursor
	}
}

func TestDefaultRegistryIncludesBakedSourceTypes(t *testing.T) {
	registry := DefaultRegistry(Options{})
	if _, ok := registry["synthetic"]; !ok {
		t.Errorf("registry lost the synthetic factory")
	}
	if _, ok := registry[tsdb.SourceType]; !ok {
		t.Errorf("registry lost the %s factory", tsdb.SourceType)
	}
	if _, ok := registry[espn.SourceType]; !ok {
		t.Errorf("registry lost the %s factory", espn.SourceType)
	}
	if len(registry) != 3 {
		t.Errorf("registry size = %d, want 3", len(registry))
	}
}

func TestSyntheticWalkIsDeterministicAndComplete(t *testing.T) {
	factory := Factory()
	source, err := factory(extract.SourceDef{ID: "synthetic", Type: "synthetic", Streams: []string{"snapshot"}})
	if err != nil {
		t.Fatalf("build source: %v", err)
	}
	first, firstPages := walkAll(t, source)
	second, secondPages := walkAll(t, source)
	if firstPages != secondPages {
		t.Fatalf("page counts differ: %d vs %d", firstPages, secondPages)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("datasets differ between two independent walks")
	}

	counts := map[model.Kind]int{}
	for _, row := range first {
		counts[row.Kind]++
	}
	want := map[model.Kind]int{
		model.KindLeague: 3,
		model.KindTeam:   7,
		model.KindPlayer: 8,
		model.KindGame:   8,
		model.KindStat:   25,
	}
	for kind, expected := range want {
		if counts[kind] != expected {
			t.Errorf("kind %d: rows = %d, want %d", kind, counts[kind], expected)
		}
	}
	if len(first) != 51 {
		t.Errorf("total rows = %d, want 51", len(first))
	}
}

func TestSyntheticReferentialPrerequisites(t *testing.T) {
	factory := Factory()
	source, _ := factory(extract.SourceDef{ID: "synthetic", Type: "synthetic", Streams: []string{"snapshot"}})
	var all []model.Row
	cursor := map[string]any(nil)
	for {
		wait, err := source.Fetch(context.Background(), "snapshot", cursor)
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		all = append(all, wait.Rows...)
		if wait.Done {
			break
		}
		cursor = wait.Cursor
	}

	teams := map[string]bool{}
	players := map[string]bool{}
	games := map[string]bool{}
	for _, row := range all {
		switch row.Kind {
		case model.KindTeam:
			teams[row.Team.LeagueCode+"/"+row.Team.Code] = true
		case model.KindPlayer:
			players[row.Player.LeagueCode+"/"+row.Player.Code] = true
			if !teams[row.Player.LeagueCode+"/"+row.Player.TeamCode] {
				t.Errorf("player %s references unknown team %s", row.Player.Code, row.Player.TeamCode)
			}
		case model.KindGame:
			games[row.Game.LeagueCode+"/"+row.Game.GameCode] = true
			if !teams[row.Game.LeagueCode+"/"+row.Game.HomeTeamCode] {
				t.Errorf("game %s references unknown home team %s", row.Game.GameCode, row.Game.HomeTeamCode)
			}
			if !teams[row.Game.LeagueCode+"/"+row.Game.AwayTeamCode] {
				t.Errorf("game %s references unknown away team %s", row.Game.GameCode, row.Game.AwayTeamCode)
			}
		case model.KindStat:
			if !games[row.Stat.LeagueCode+"/"+row.Stat.GameCode] {
				t.Errorf("stat %s/%s references unknown game", row.Stat.GameCode, row.Stat.PlayerCode)
			}
			if !players[row.Stat.LeagueCode+"/"+row.Stat.PlayerCode] {
				t.Errorf("stat %s/%s references unknown player", row.Stat.GameCode, row.Stat.PlayerCode)
			}
		}
	}
}

func TestSyntheticRejectsUnknownStreamAndBadCursor(t *testing.T) {
	factory := Factory()
	source, _ := factory(extract.SourceDef{ID: "synthetic", Type: "synthetic", Streams: []string{"snapshot"}})
	if _, err := source.Fetch(context.Background(), "nope", nil); err == nil {
		t.Fatalf("unknown stream must fail")
	}
	if _, err := source.Fetch(context.Background(), "snapshot", map[string]any{"page": -1}); err == nil {
		t.Fatalf("negative page must fail")
	}
	if _, err := source.Fetch(context.Background(), "snapshot", map[string]any{"page": "x"}); err == nil {
		t.Fatalf("non-numeric page must fail")
	}
}

func TestSyntheticAcceptsFloatCursor(t *testing.T) {
	factory := Factory()
	source, _ := factory(extract.SourceDef{ID: "synthetic", Type: "synthetic", Streams: []string{"snapshot"}})
	batch, err := source.Fetch(context.Background(), "snapshot", map[string]any{"page": float64(2)})
	if err != nil {
		t.Fatalf("float64 cursor rejected: %v", err)
	}
	if len(batch.Rows) != 8 {
		t.Fatalf("page 2 rows = %d, want 8 (games)", len(batch.Rows))
	}
}
