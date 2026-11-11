package source

import (
	"context"
	"time"
)

// Game is a single NBA game as reported by a source.
type Game struct {
	Date      time.Time
	StartsAt  time.Time
	Season    string //start year, doesnt follow 2025-26 style date format
	Status    string // scheduled | live | final | postponed | canceled
	HomeTeam  Team
	AwayTeam  Team
	HomeScore *int
	AwayScore *int
	Venue     string
	GameID    string // source-stable id, used as games.game_code and boxscore key
}

// Team is a league team. Code is the stable used as teams.code.
type Team struct {
	Code         string
	Name         string
	Abbreviation string
	Conference   string
	Division     string
}

// PlayerLine is one player's box-score line for one game.
type PlayerLine struct {
	GameID    string
	Player    Player
	Minutes   *int // nil for did-not-play
	Points    int
	Rebounds  int
	Assists   int
	Steals    int
	Blocks    int
	Turnovers int
	FgMade    int
	FgAtt     int
	ThreeMade int
	ThreeAtt  int
	FtMade    int
	FtAtt     int
}

// Player is a single player. Code is the stable slug used as players.code.
type Player struct {
	Code     string
	FullName string
	TeamCode string
	Position string
}

// Source is the one contract the ingestor programs against.
type Source interface {
	// GamesOn returns every NBA game on the given date (UTC calendar day).
	GamesOn(ctx context.Context, date time.Time) ([]Game, error)
	// BoxScore returns every player line for the given game id.
	BoxScore(ctx context.Context, gameID string) ([]PlayerLine, error)
}
