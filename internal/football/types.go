package football

// Types for the API-Football (api-sports.io) v3 API.
// Docs: https://www.api-football.com/documentation-v3

// Fixture is one entry in the /fixtures `response` array.
type Fixture struct {
	Fixture FixtureInfo `json:"fixture"`
	League  LeagueInfo  `json:"league"`
	Teams   struct {
		Home Team `json:"home"`
		Away Team `json:"away"`
	} `json:"teams"`
	Goals struct {
		Home *int `json:"home"`
		Away *int `json:"away"`
	} `json:"goals"`
}

// FixtureInfo holds the core fixture fields.
type FixtureInfo struct {
	ID     int    `json:"id"`
	Date   string `json:"date"` // ISO8601 with offset, e.g. 2026-07-02T19:00:00+00:00
	Status struct {
		Long    string `json:"long"`  // "Match Finished", "Second Half", ...
		Short   string `json:"short"` // NS, 1H, HT, 2H, FT, ...
		Elapsed *int   `json:"elapsed"`
	} `json:"status"`
	Venue struct {
		Name string `json:"name"`
		City string `json:"city"`
	} `json:"venue"`
}

// LeagueInfo is the league block on a fixture.
type LeagueInfo struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Country string `json:"country"`
	Season  int    `json:"season"`
	Round   string `json:"round"`
}

// Team is a team reference.
type Team struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Logo   string `json:"logo"`
	Winner *bool  `json:"winner"`
}

// League is one entry in the /leagues `response` array.
type League struct {
	League struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
		Logo string `json:"logo"`
	} `json:"league"`
	Country struct {
		Name string `json:"name"`
		Code string `json:"code"`
	} `json:"country"`
}

// StandingsResponse is one entry in the /standings `response` array.
type StandingsResponse struct {
	League struct {
		ID        int             `json:"id"`
		Name      string          `json:"name"`
		Season    int             `json:"season"`
		Standings [][]StandingRow `json:"standings"` // groups -> rows
	} `json:"league"`
}

// StandingRow is one team's row in a standings table.
type StandingRow struct {
	Rank      int    `json:"rank"`
	Team      Team   `json:"team"`
	Points    int    `json:"points"`
	GoalsDiff int    `json:"goalsDiff"`
	Form      string `json:"form"`
	All       struct {
		Played int `json:"played"`
		Win    int `json:"win"`
		Draw   int `json:"draw"`
		Lose   int `json:"lose"`
		Goals  struct {
			For     int `json:"for"`
			Against int `json:"against"`
		} `json:"goals"`
	} `json:"all"`
}
