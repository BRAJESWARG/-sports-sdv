package football

import "encoding/json"

// SportMonks Football API v3 response shapes.
//
// Unlike Cricket v2, v3 does NOT wrap includes in {"data": ...} — related
// resources are inline arrays/objects on the parent. Docs:
// https://docs.sportmonks.com/football

// Fixture is a football match.
type Fixture struct {
	ID         int64   `json:"id"`
	LeagueID   int64   `json:"league_id"`
	SeasonID   int64   `json:"season_id"`
	StateID    int     `json:"state_id"`
	RoundID    int64   `json:"round_id"`
	Name       string  `json:"name"` // "Home vs Away"
	StartingAt string  `json:"starting_at"`
	ResultInfo *string `json:"result_info"`

	// includes (present only when requested via include=)
	Participants []Participant `json:"participants,omitempty"`
	Scores       []Score       `json:"scores,omitempty"`
	State        *State        `json:"state,omitempty"`
	League       *League       `json:"league,omitempty"`
	Round        *Round        `json:"round,omitempty"`
}

// Participant is a team within a fixture; Meta carries home/away + result.
type Participant struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	ShortCode string          `json:"short_code"`
	ImagePath string          `json:"image_path"`
	Meta      ParticipantMeta `json:"meta"`
}

// ParticipantMeta describes a team's role in a fixture.
type ParticipantMeta struct {
	Location string `json:"location"` // "home" | "away"
	Winner   *bool  `json:"winner"`
	Position int    `json:"position"`
}

// Score is one scoreline entry for a fixture.
type Score struct {
	ParticipantID int64      `json:"participant_id"`
	Description   string     `json:"description"` // "CURRENT", "1ST_HALF", "2ND_HALF", ...
	Score         ScoreValue `json:"score"`
}

// ScoreValue holds the goals for a participant side.
type ScoreValue struct {
	Goals       int    `json:"goals"`
	Participant string `json:"participant"` // "home" | "away"
}

// State is the match status.
type State struct {
	ID        int    `json:"id"`
	State     string `json:"state"` // "NS", "INPLAY_2ND_HALF", "FT", ...
	Name      string `json:"name"`  // "Not Started", "2nd Half", "Full Time"
	ShortName string `json:"short_name"`
}

// League is a competition.
type League struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ImagePath string `json:"image_path"`
	CountryID int64  `json:"country_id"`
}

// Round is a matchday/round.
type Round struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Team is the trimmed team object returned by include=participant on standings.
type Team struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ShortCode string `json:"short_code"`
	ImagePath string `json:"image_path"`
}

// Standing is one row of a season standings table.
type Standing struct {
	Position      int              `json:"position"`
	ParticipantID int64            `json:"participant_id"`
	Points        int              `json:"points"`
	Participant   *Team            `json:"participant,omitempty"` // via include=participant
	Details       []StandingDetail `json:"details,omitempty"`     // via include=details
}

// StandingDetail is a keyed stat (played/won/... ) identified by type_id.
type StandingDetail struct {
	TypeID int         `json:"type_id"`
	Value  json.Number `json:"value"`
}
