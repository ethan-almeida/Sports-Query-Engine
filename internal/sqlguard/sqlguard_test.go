package sqlguard

import (
	"strings"
	"testing"
)

func TestCheckAcceptsSingleSelect(t *testing.T) {
	sql, err := Check("SELECT full_name, points FROM player_game_stats LIMIT 5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(sql, "SELECT * FROM (SELECT full_name") {
		t.Fatalf("query should be wrapped in outer select, got: %s", sql)
	}
	if !strings.HasSuffix(sql, "LIMIT 200") {
		t.Fatalf("query should end with LIMIT 200, got: %s", sql)
	}
}

func TestCheckStripsFenceAndSemicolon(t *testing.T) {
	sql, err := Check("```sql\nSELECT COUNT(*) FROM games;\n```")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(sql, "SELECT COUNT(*) FROM games") {
		t.Fatalf("fence/semicolon not stripped, got: %s", sql)
	}
}

func TestCheckRejectsNonSelect(t *testing.T) {
	bad := []string{
		"DROP TABLE players",
		"DELETE FROM games",
		"INSERT INTO teams (code) VALUES ('x')",
		"UPDATE players SET position = 'C'",
		"SELECT * FROM players; DROP TABLE games",
		"SELECT * FROM players -- leak comment",
		"SELECT pg_sleep(100)",
		"SELECT * FROM games /* block */",
	}
	for _, in := range bad {
		if _, err := Check(in); err == nil {
			t.Errorf("expected rejection for %q, got none", in)
		}
	}
}

func TestCheckRejectsToolCallNoise(t *testing.T) {
	if _, err := Check("<tool_call><function=router>[]"); err == nil {
		t.Fatal("expected rejection for tool-call noise, got none")
	}
}
