package sportmonks

import (
	"bytes"
	"encoding/json"
)

// SportMonks Cricket API v2.0 usually wraps included relations in a nested
// {"data": ...} object, but not consistently — some endpoints (e.g. livescores)
// return the relation as a bare array/object. Data[T] models the wrapper
// generically and its UnmarshalJSON tolerates both shapes:
//
//	wrapped: "localteam": {"data": {...}}   => *Data[Team]
//	wrapped: "runs":      {"data": [...]}   => *Data[[]Run]
//	bare:    "runs":      [...]             => *Data[[]Run]
type Data[T any] struct {
	Data T `json:"data"`
}

// UnmarshalJSON accepts either the {"data": <T>} wrapper or a bare <T> payload.
func (d *Data[T]) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	// Wrapped form: an object carrying a "data" key.
	if trimmed[0] == '{' {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &probe); err == nil {
			if inner, ok := probe["data"]; ok {
				return json.Unmarshal(inner, &d.Data)
			}
		}
	}
	// Bare form: the payload itself is the T (array, or object without a wrapper).
	return json.Unmarshal(trimmed, &d.Data)
}

// Team is a cricket team/nation.
type Team struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	ImagePath string `json:"image_path"`
}

// Player is a cricketer.
type Player struct {
	ID        int    `json:"id"`
	Fullname  string `json:"fullname"`
	Firstname string `json:"firstname"`
	Lastname  string `json:"lastname"`
}

// Fixture is a cricket match. Fields under the "include" comment are only
// populated when the corresponding include= is requested.
type Fixture struct {
	ID            int    `json:"id"`
	LeagueID      int    `json:"league_id"`
	SeasonID      int    `json:"season_id"`
	StageID       int    `json:"stage_id"`
	Round         string `json:"round"`
	LocalTeamID   int    `json:"localteam_id"`
	VisitorTeamID int    `json:"visitorteam_id"`
	StartingAt    string `json:"starting_at"`
	Type          string `json:"type"` // "T20I", "ODI", "Test", ...
	Live          bool   `json:"live"`
	Status        string `json:"status"` // "NS", "1st Innings", "Finished", ...
	Note          string `json:"note"`   // human-readable result summary
	VenueID       int    `json:"venue_id"`
	WinnerTeamID  *int   `json:"winner_team_id"`

	// includes
	League      *Data[League]    `json:"league,omitempty"`
	LocalTeam   *Data[Team]      `json:"localteam,omitempty"`
	VisitorTeam *Data[Team]      `json:"visitorteam,omitempty"`
	Runs        *Data[[]Run]     `json:"runs,omitempty"`
	Batting     *Data[[]Batting] `json:"batting,omitempty"`
	Bowling     *Data[[]Bowling] `json:"bowling,omitempty"`
}

// Run is one innings total (via include=runs).
type Run struct {
	TeamID  int     `json:"team_id"`
	Inning  int     `json:"inning"`
	Score   int     `json:"score"`
	Wickets int     `json:"wickets"`
	Overs   float64 `json:"overs"`
}

// Batting is one batsman's line in the scorecard (via include=batting).
type Batting struct {
	TeamID     int           `json:"team_id"`
	PlayerID   int           `json:"player_id"`
	Score      int           `json:"score"`
	Ball       int           `json:"ball"`
	FourX      int           `json:"four_x"`
	SixX       int           `json:"six_x"`
	Rate       float64       `json:"rate"`
	Active     bool          `json:"active"`     // true only for the striker
	Scoreboard string        `json:"scoreboard"` // "S1" (1st innings), "S2" (2nd), ...
	Sort       int           `json:"sort"`
	FowScore   int           `json:"fow_score"` // team score at fall of wicket; 0 => not out
	FowBalls   float64       `json:"fow_balls"` // over at fall of wicket; 0 => not out
	Batsman    *Data[Player] `json:"batsman,omitempty"`
}

// Bowling is one bowler's line in the scorecard (via include=bowling).
type Bowling struct {
	TeamID     int           `json:"team_id"`
	PlayerID   int           `json:"player_id"`
	Overs      float64       `json:"overs"`
	Runs       int           `json:"runs"`
	Wickets    int           `json:"wickets"`
	Wide       int           `json:"wide"`
	Noball     int           `json:"noball"`
	Rate       float64       `json:"rate"`       // economy
	Active     bool          `json:"active"`     // currently bowling
	Scoreboard string        `json:"scoreboard"` // "S1", "S2", ...
	Sort       int           `json:"sort"`
	Bowler     *Data[Player] `json:"bowler,omitempty"`
}

// Standing is one row of a season standings table (/standings/season/{id}).
type Standing struct {
	SeasonID int         `json:"season_id"`
	Position int         `json:"position"`
	TeamID   int         `json:"team_id"`
	Points   int         `json:"points"`
	Won      int         `json:"won"`
	Lost     int         `json:"lost"`
	Draw     int         `json:"draw"`
	NoResult int         `json:"noresult"`
	Team     *Data[Team] `json:"team,omitempty"`
}

// RankingType is one ICC ranking table (a type+gender) from /team-rankings.
type RankingType struct {
	Type   string              `json:"type"`   // TEST, ODI, T20I
	Gender string              `json:"gender"` // men, women
	Team   *Data[[]RankedTeam] `json:"team,omitempty"`
}

// RankedTeam is a nation within a ranking table. The ranking stats live in a
// nested "ranking" object; only position/name/code are on the team itself.
type RankedTeam struct {
	ID       int          `json:"id"`
	Name     string       `json:"name"`
	Code     string       `json:"code"`
	Position int          `json:"position"`
	Ranking  RankingStats `json:"ranking"`
}

// RankingStats holds the per-team ranking numbers from the nested "ranking" object.
type RankingStats struct {
	Position int `json:"position"`
	Matches  int `json:"matches"`
	Points   int `json:"points"`
	Rating   int `json:"rating"`
}

// League is a competition (Test/ODI/T20I series, IPL, etc.).
type League struct {
	ID        int    `json:"id"`
	SeasonID  int    `json:"season_id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Type      string `json:"type"`
	ImagePath string `json:"image_path"`
}

// Season is a competition season.
type Season struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	LeagueID int    `json:"league_id"`
	Code     string `json:"code"`
}
