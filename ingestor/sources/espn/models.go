package espn

type apiTeam struct {
	ID               string `json:"id"`
	Abbreviation     string `json:"abbreviation"`
	DisplayName      string `json:"displayName"`
	ShortDisplayName string `json:"shortDisplayName"`
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	IsActive         bool   `json:"isActive"`
}

type teamsListPayload struct {
	Sports []struct {
		Leagues []struct {
			Name  string `json:"name"`
			Teams []struct {
				Team apiTeam `json:"team"`
			} `json:"teams"`
		} `json:"leagues"`
	} `json:"sports"`
}

type apiPosition struct {
	Abbreviation string `json:"abbreviation"`
	DisplayName  string `json:"displayName"`
	Name         string `json:"name"`
}

type apiAthlete struct {
	ID          string      `json:"id"`
	FullName    string      `json:"fullName"`
	DisplayName string      `json:"displayName"`
	ShortName   string      `json:"shortName"`
	Position    apiPosition `json:"position"`
}

type rosterPayload struct {
	Team struct {
		ID       string       `json:"id"`
		Athletes []apiAthlete `json:"athletes"`
	} `json:"team"`
}

type apiStatusType struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type apiCompetitor struct {
	HomeAway string `json:"homeAway"`
	Score    string `json:"score"`
	Team     struct {
		ID           string `json:"id"`
		Abbreviation string `json:"abbreviation"`
		DisplayName  string `json:"displayName"`
		Name         string `json:"name"`
	} `json:"team"`
}

type apiCompetition struct {
	Venue struct {
		FullName string `json:"fullName"`
	} `json:"venue"`
	Season      *int            `json:"season"`
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

type seasonPayload struct {
	Year      int    `json:"year"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}
