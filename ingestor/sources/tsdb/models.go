package tsdb

type apiLeague struct {
	ID        string `json:"idLeague"`
	Name      string `json:"strLeague"`
	Sport     string `json:"strSport"`
	Alternate string `json:"strLeagueAlternate"`
}

type apiTeam struct {
	ID    string `json:"idTeam"`
	Name  string `json:"strTeam"`
	Short string `json:"strTeamShort"`
}

type apiPlayer struct {
	ID       string `json:"idPlayer"`
	Name     string `json:"strPlayer"`
	TeamID   string `json:"idTeam"`
	Team     string `json:"strTeam"`
	Position string `json:"strPosition"`
}

type apiEvent struct {
	ID        string `json:"idEvent"`
	Name      string `json:"strEvent"`
	Season    string `json:"strSeason"`
	LeagueID  string `json:"idLeague"`
	League    string `json:"strLeague"`
	Home      string `json:"strHomeTeam"`
	Away      string `json:"strAwayTeam"`
	HomeScore string `json:"intHomeScore"`
	AwayScore string `json:"intAwayScore"`
	Date      string `json:"dateEvent"`
	Time      string `json:"strTime"`
	Timestamp string `json:"strTimestamp"`
	Status    string `json:"strStatus"`
	Postponed string `json:"strPostponed"`
	Venue     string `json:"strVenue"`
}

type leaguesPayload struct {
	Leagues []apiLeague `json:"leagues"`
}

type teamsPayload struct {
	Teams []apiTeam `json:"teams"`
}

type playersPayload struct {
	Player []apiPlayer `json:"player"`
}

type eventsPayload struct {
	Events []apiEvent `json:"events"`
}
