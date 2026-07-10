// Package highlightly is a thin, typed client for the Highlightly sports APIs
// (football at sports.highlightly.net/football, cricket at cricket.highlightly.net).
// Auth is via the x-rapidapi-key header. It does not cache — that is the
// service's job.
package highlightly

// listEnvelope wraps Highlightly list responses: {data:[...], plan, pagination}.
type listEnvelope[T any] struct {
	Data       []T          `json:"data"`
	Pagination hlPagination `json:"pagination"`
}

// hlPagination is the paging block; totalCount can exceed the 100-row page limit.
type hlPagination struct {
	TotalCount int `json:"totalCount"`
	Offset     int `json:"offset"`
	Limit      int `json:"limit"`
}

// ---- Football ----

// FootballMatch is one football fixture/result.
type FootballMatch struct {
	ID       int64          `json:"id"`
	Round    string         `json:"round"`
	Date     string         `json:"date"` // ISO, e.g. "2026-07-07T18:15:00.000Z"
	State    FootballState  `json:"state"`
	HomeTeam FootballTeam   `json:"homeTeam"`
	AwayTeam FootballTeam   `json:"awayTeam"`
	League   FootballLeague `json:"league"`
}

type FootballState struct {
	Clock       *int          `json:"clock"`
	Description string        `json:"description"` // "Not started" | "First half" | "Finished" | ...
	Score       FootballScore `json:"score"`
}

type FootballScore struct {
	Current string `json:"current"` // "0 - 3" (empty/"-" before kickoff)
}

type FootballTeam struct {
	Name string `json:"name"`
}

type FootballLeague struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Season int    `json:"season"`
}

// ---- Cricket ----

// CricketMatch is one cricket fixture/result.
type CricketMatch struct {
	ID        string        `json:"id"` // string ids upstream
	StartDate string        `json:"startDate"`
	StartTime string        `json:"startTime"` // ISO kickoff
	Format    string        `json:"format"`    // "T20" | "ODI" | "Test"
	State     CricketState  `json:"state"`
	HomeTeam  CricketTeam   `json:"homeTeam"`
	AwayTeam  CricketTeam   `json:"awayTeam"`
	League    CricketLeague `json:"league"`
}

type CricketState struct {
	Description string            `json:"description"` // "Not started" | "Finished" | in-play states
	Report      string            `json:"report"`      // "Diamonds won by 73 runs"
	Teams       CricketStateTeams `json:"teams"`
}

type CricketStateTeams struct {
	Home CricketInnings `json:"home"`
	Away CricketInnings `json:"away"`
}

type CricketInnings struct {
	Info  string `json:"info"`  // "20 ov, T:217" (may be null)
	Score string `json:"score"` // "216/5" (may be null)
}

type CricketTeam struct {
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
}

type CricketLeague struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Season int    `json:"season"`
}
