package footballdata

// Types for the football-data.org v4 API.
// Docs: https://docs.football-data.org/general/v4/

// Match is a football-data.org match.
type Match struct {
	ID          int64       `json:"id"`
	UtcDate     string      `json:"utcDate"`
	Status      string      `json:"status"` // SCHEDULED, TIMED, IN_PLAY, PAUSED, FINISHED, POSTPONED, ...
	Matchday    int         `json:"matchday"`
	Minute      *int        `json:"minute"`
	Competition Competition `json:"competition"`
	Season      Season      `json:"season"`
	HomeTeam    TeamRef     `json:"homeTeam"`
	AwayTeam    TeamRef     `json:"awayTeam"`
	Score       Score       `json:"score"`
}

// Competition is a league/cup.
type Competition struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Code   string `json:"code"` // e.g. "PL", "PD", "BL1"
	Emblem string `json:"emblem"`
}

// Season describes the season a match/standing belongs to.
type Season struct {
	ID              int64  `json:"id"`
	StartDate       string `json:"startDate"`
	EndDate         string `json:"endDate"`
	CurrentMatchday int    `json:"currentMatchday"`
}

// TeamRef is the trimmed team object used across matches/standings.
type TeamRef struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`
	Tla       string `json:"tla"`
	Crest     string `json:"crest"`
}

// Score holds the match scoreline.
type Score struct {
	Winner   string     `json:"winner"` // HOME_TEAM, AWAY_TEAM, DRAW, or null
	Duration string     `json:"duration"`
	FullTime ScoreGoals `json:"fullTime"` // current running score during play; final at end
	HalfTime ScoreGoals `json:"halfTime"`
}

// ScoreGoals is a home/away goal pair (nil until the match starts).
type ScoreGoals struct {
	Home *int `json:"home"`
	Away *int `json:"away"`
}

// ---- response envelopes ----

type matchesEnvelope struct {
	Matches []Match `json:"matches"`
}

type competitionsEnvelope struct {
	Competitions []Competition `json:"competitions"`
}

// StandingsResponse is the /competitions/{code}/standings payload.
type StandingsResponse struct {
	Competition Competition      `json:"competition"`
	Season      Season           `json:"season"`
	Standings   []StandingsGroup `json:"standings"`
}

// StandingsGroup is one standings table (TOTAL / HOME / AWAY).
type StandingsGroup struct {
	Stage string        `json:"stage"`
	Type  string        `json:"type"`
	Table []StandingRow `json:"table"`
}

// StandingRow is one team's row in a standings table.
type StandingRow struct {
	Position       int     `json:"position"`
	Team           TeamRef `json:"team"`
	PlayedGames    int     `json:"playedGames"`
	Won            int     `json:"won"`
	Draw           int     `json:"draw"`
	Lost           int     `json:"lost"`
	Points         int     `json:"points"`
	GoalsFor       int     `json:"goalsFor"`
	GoalsAgainst   int     `json:"goalsAgainst"`
	GoalDifference int     `json:"goalDifference"`
}
