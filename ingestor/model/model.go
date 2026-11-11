package model

import "time"

type League struct {
	Code  string
	Name  string
	Sport string
}

type Team struct {
	LeagueCode   string
	Code         string
	Name         string
	Abbreviation string
	Conference   string
	Division     string
}

type Player struct {
	LeagueCode string
	Code       string
	FullName   string
	TeamCode   string
	Position   string
	Active     bool
}

type Game struct {
	LeagueCode   string
	GameCode     string
	Season       string
	StartsAt     time.Time
	Status       string
	HomeTeamCode string
	AwayTeamCode string
	HomeScore    *int
	AwayScore    *int
	Venue        string
}

type Stat struct {
	LeagueCode string
	GameCode   string
	PlayerCode string
	StatName   string
	StatValue  float64
}

type Kind int

const (
	KindLeague Kind = iota + 1
	KindTeam
	KindPlayer
	KindGame
	KindStat
)

type Row struct {
	Kind   Kind
	League *League
	Team   *Team
	Player *Player
	Game   *Game
	Stat   *Stat
}
