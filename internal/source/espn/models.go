package espn

// ESPN scoreboard JSON payload (site.api.espn.com).
//
// The clean output types (Game, Team, Player, PlayerLine) live in the
// source package; espn implements source.Source and returns those types.

type apiStatusType struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Completed bool   `json:"completed"`
}

type apiCompetitorTeam struct {
	ID           string `json:"id"`
	Abbreviation string `json:"abbreviation"`
	DisplayName  string `json:"displayName"`
	Name         string `json:"name"`
}

type apiCompetitor struct {
	HomeAway string            `json:"homeAway"`
	Score    string            `json:"score"`
	Team     apiCompetitorTeam `json:"team"`
}

type apiCompetition struct {
	Venue struct {
		FullName string `json:"fullName"`
	} `json:"venue"`
	Competitors []apiCompetitor `json:"competitors"`
}

type apiEvent struct {
	ID    string `json:"id"`
	Date  string `json:"date"`
	Venue struct {
		FullName string `json:"fullName"`
	} `json:"venue"`
	Status struct {
		Type apiStatusType `json:"type"`
	} `json:"status"`
	Competitions []apiCompetition `json:"competitions"`
}

type scoreboardPayload struct {
	Leagues []struct {
		Name string `json:"name"`
	} `json:"leagues"`
	Events []apiEvent `json:"events"`
}

// ESPN game summary JSON payload (site.api.espn.com/.../summary?event={id}).
// The box score lives under boxscore.players[]: one entry per team, each with
// a statistics group whose keys[] line up with every athlete's stats[].

type summaryPayload struct {
	Boxscore struct {
		Players []summaryPlayerBox `json:"players"`
	} `json:"boxscore"`
}

type summaryPlayerBox struct {
	Team       summaryTeam        `json:"team"`
	Statistics []summaryStatGroup `json:"statistics"`
}

type summaryTeam struct {
	Abbreviation string `json:"abbreviation"`
}

type summaryStatGroup struct {
	Keys     []string         `json:"keys"`
	Athletes []summaryAthlete `json:"athletes"`
}

type summaryAthlete struct {
	DidNotPlay bool     `json:"didNotPlay"`
	Stats      []string `json:"stats"`
	Athlete    struct {
		DisplayName string `json:"displayName"`
		Position    struct {
			Abbreviation string `json:"abbreviation"`
		} `json:"position"`
	} `json:"athlete"`
}
