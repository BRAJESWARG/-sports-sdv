package sports

// Football DTOs — your API's public contract for SportMonks Football (v3),
// decoupled from the upstream football.* types.

// FootballMatchDTO is a football match summary (live, schedule, results).
type FootballMatchDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`      // e.g. "Full Time", "2nd Half"
	StatusShort string `json:"statusShort"` // e.g. "FT", "NS", "INPLAY_2ND_HALF"
	StartingAt  string `json:"startingAt"`
	LeagueID    int64  `json:"leagueId"`
	SeasonID    int64  `json:"seasonId"`
	ResultInfo  string `json:"resultInfo,omitempty"`
	HomeTeam    string `json:"homeTeam"`
	AwayTeam    string `json:"awayTeam"`
	HomeGoals   *int   `json:"homeGoals,omitempty"`
	AwayGoals   *int   `json:"awayGoals,omitempty"`
}

// FootballStandingDTO is one row of a league table.
type FootballStandingDTO struct {
	Position     int    `json:"position"`
	Team         string `json:"team"`
	TeamID       int64  `json:"teamId"`
	Points       int    `json:"points"`
	Played       int    `json:"played"`
	Won          int    `json:"won"`
	Draw         int    `json:"draw"`
	Lost         int    `json:"lost"`
	GoalsFor     int    `json:"goalsFor"`
	GoalsAgainst int    `json:"goalsAgainst"`
	GoalDiff     int    `json:"goalDifference"`
}

// FootballLeagueDTO is a competition.
type FootballLeagueDTO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ImagePath string `json:"imagePath,omitempty"`
}
