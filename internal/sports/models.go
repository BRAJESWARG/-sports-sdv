package sports

// The types in this file are YOUR API's contract — the cricket shapes you
// re-expose to clients, decoupled from the SportMonks upstream types so you can
// change providers later without breaking consumers.

// InningsDTO is one innings total in a match.
type InningsDTO struct {
	Team    string  `json:"team"`
	Runs    int     `json:"runs"`
	Wickets int     `json:"wickets"`
	Overs   float64 `json:"overs"`
}

// MatchDTO is the match summary your API returns (live, schedule, results).
type MatchDTO struct {
	ID          int          `json:"id"`
	Type        string       `json:"type"` // T20I / ODI / Test
	Status      string       `json:"status"`
	Live        bool         `json:"live"`
	StartingAt  string       `json:"startingAt"`
	LeagueID    int          `json:"leagueId"`
	SeasonID    int          `json:"seasonId"`
	Round       string       `json:"round,omitempty"`
	Note        string       `json:"note,omitempty"` // result summary when finished
	LocalTeam   string       `json:"localTeam"`
	VisitorTeam string       `json:"visitorTeam"`
	Innings     []InningsDTO `json:"innings,omitempty"`

	// Live-only enrichment (populated for in-progress matches).
	BattingTeam string       `json:"battingTeam,omitempty"`
	Overs       float64      `json:"overs,omitempty"`           // current innings overs (X.Y)
	CRR         float64      `json:"currentRunRate,omitempty"`  // current run rate
	RRR         float64      `json:"requiredRunRate,omitempty"` // required run rate (chase)
	Required    int          `json:"requiredRuns,omitempty"`    // runs still needed (chase)
	Batsmen     []BattingDTO `json:"batsmen,omitempty"`         // current not-out pair
	Bowler      *BowlingDTO  `json:"bowler,omitempty"`          // current bowler
}

// BattingDTO is one batsman's scorecard line.
type BattingDTO struct {
	Player     string  `json:"player"`
	TeamID     int     `json:"teamId"`
	Runs       int     `json:"runs"`
	Balls      int     `json:"balls"`
	Fours      int     `json:"fours"`
	Sixes      int     `json:"sixes"`
	StrikeRate float64 `json:"strikeRate"`
	OnStrike   bool    `json:"onStrike,omitempty"` // live: currently on strike
}

// BowlingDTO is one bowler's scorecard line.
type BowlingDTO struct {
	Player  string  `json:"player"`
	TeamID  int     `json:"teamId"`
	Overs   float64 `json:"overs"`
	Runs    int     `json:"runs"`
	Wickets int     `json:"wickets"`
	Economy float64 `json:"economy"`
}

// ScorecardDTO is the detailed per-innings scorecard for a match.
type ScorecardDTO struct {
	MatchID     int          `json:"matchId"`
	Type        string       `json:"type"`
	Status      string       `json:"status"`
	Note        string       `json:"note,omitempty"`
	LocalTeam   string       `json:"localTeam"`
	VisitorTeam string       `json:"visitorTeam"`
	Innings     []InningsDTO `json:"innings"`
	Batting     []BattingDTO `json:"batting"`
	Bowling     []BowlingDTO `json:"bowling"`
}

// StandingDTO is one row of a season standings table.
type StandingDTO struct {
	Position int    `json:"position"`
	Team     string `json:"team"`
	TeamID   int    `json:"teamId"`
	Points   int    `json:"points"`
	Won      int    `json:"won"`
	Lost     int    `json:"lost"`
	Draw     int    `json:"draw"`
	NoResult int    `json:"noResult"`
}

// RankedTeamDTO is a nation's slot within a ranking table.
type RankedTeamDTO struct {
	Position int    `json:"position"`
	Team     string `json:"team"`
	Matches  int    `json:"matches"`
	Rating   int    `json:"rating"`
	Points   int    `json:"points"`
}

// RankingDTO is one ICC ranking table (a format + gender).
type RankingDTO struct {
	Type   string          `json:"type"`   // TEST / ODI / T20I
	Gender string          `json:"gender"` // men / women
	Teams  []RankedTeamDTO `json:"teams"`
}

// LeagueDTO is a competition.
type LeagueDTO struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Code string `json:"code,omitempty"`
	Type string `json:"type,omitempty"`
}
