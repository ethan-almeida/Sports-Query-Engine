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

// ESPN box-score HTML page. There is no JSON box-score endpoint anymore; the
// data is server-rendered into the page as window['__espnfitt__'] and we pull
// the bxscr array out of it.

type fittPage struct {
	Page struct {
		Content struct {
			GamePackage struct {
				Bxscr []bxscrTeamBox `json:"bxscr"`
			} `json:"gamepackage"`
		} `json:"content"`
	} `json:"page"`
}

type bxscrTeamBox struct {
	Tm    bxscrTeam    `json:"tm"`
	Stats []bxscrGroup `json:"stats"`
}

type bxscrTeam struct {
	Abbrev string `json:"abbrev"`
	Name   string `json:"nm"`
	Home   bool   `json:"hm"`
	ID     string `json:"id"`
}

type bxscrGroup struct {
	Type   string         `json:"type"`
	Keys   []string       `json:"keys"`
	Athlts []bxscrAthlete `json:"athlts"`
}

type bxscrAthlete struct {
	Stats []string `json:"stats"`
	Athlt struct {
		ID   string `json:"id"`
		Name string `json:"dspNm"`
		Pos  string `json:"pos"`
	} `json:"athlt"`
}
