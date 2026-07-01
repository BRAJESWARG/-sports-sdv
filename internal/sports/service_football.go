package sports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/bgmaster/sports-sdv/internal/cache"
	"github.com/bgmaster/sports-sdv/internal/football"
)

// FootballService orchestrates the SportMonks Football (v3) client + cache and
// maps upstream types to football DTOs.
type FootballService struct {
	client  *football.Client
	cache   cache.Cache
	ttl     time.Duration
	ttlLive time.Duration
}

// NewFootball wires the football service.
func NewFootball(client *football.Client, c cache.Cache, ttl, ttlLive time.Duration) *FootballService {
	return &FootballService{client: client, cache: c, ttl: ttl, ttlLive: ttlLive}
}

// SportMonks v3 standings expose stats as "details" keyed by type_id. These ids
// are best-effort and SHOULD be verified against a live payload before relying
// on the played/won/draw/lost/goals columns (mock fixtures use these values).
const (
	ftTypePlayed       = 129
	ftTypeWon          = 130
	ftTypeDraw         = 131
	ftTypeLost         = 132
	ftTypeGoalsFor     = 133
	ftTypeGoalsAgainst = 134
)

// Livescores returns matches in play. Short-TTL cached.
func (fs *FootballService) Livescores(ctx context.Context) ([]FootballMatchDTO, error) {
	return cacheAside(ctx, fs.cache, "fb:livescores", fs.ttlLive, func(ctx context.Context) ([]FootballMatchDTO, error) {
		fx, err := fs.client.Livescores(ctx)
		if err != nil {
			return nil, err
		}
		return mapFootballMatches(fx), nil
	})
}

// Matches returns fixtures (schedule & results). Honors date / from+to; with no
// filter it defaults to the next 7 days so it never dumps the whole catalogue.
func (fs *FootballService) Matches(ctx context.Context, q url.Values) ([]FootballMatchDTO, error) {
	date, from, to := q.Get("date"), q.Get("from"), q.Get("to")
	key := "fb:matches?" + q.Encode()
	ttl := fs.ttl
	return cacheAside(ctx, fs.cache, key, ttl, func(ctx context.Context) ([]FootballMatchDTO, error) {
		var fx []football.Fixture
		var err error
		switch {
		case date != "":
			fx, err = fs.client.FixturesBetween(ctx, date, date)
		case from != "" && to != "":
			fx, err = fs.client.FixturesBetween(ctx, from, to)
		default:
			now := time.Now().UTC()
			fx, err = fs.client.FixturesBetween(ctx, now.Format("2006-01-02"), now.AddDate(0, 0, 7).Format("2006-01-02"))
		}
		if err != nil {
			return nil, err
		}
		return mapFootballMatches(fx), nil
	})
}

// Match returns a single match's detail.
func (fs *FootballService) Match(ctx context.Context, id int64) (*FootballMatchDTO, error) {
	key := fmt.Sprintf("fb:match?id=%d", id)
	return cacheAsidePtr(ctx, fs.cache, key, fs.ttlLive, func(ctx context.Context) (*FootballMatchDTO, error) {
		f, err := fs.client.Fixture(ctx, id)
		if err != nil {
			return nil, err
		}
		m := footballMatch(*f)
		return &m, nil
	})
}

// Standings returns the season league table, sorted by position.
func (fs *FootballService) Standings(ctx context.Context, seasonID int64) ([]FootballStandingDTO, error) {
	key := fmt.Sprintf("fb:standings?season=%d", seasonID)
	return cacheAside(ctx, fs.cache, key, fs.ttl, func(ctx context.Context) ([]FootballStandingDTO, error) {
		rows, err := fs.client.StandingsBySeason(ctx, seasonID)
		if err != nil {
			return nil, err
		}
		out := make([]FootballStandingDTO, 0, len(rows))
		for _, r := range rows {
			out = append(out, footballStanding(r))
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
		return out, nil
	})
}

// Leagues returns competitions.
func (fs *FootballService) Leagues(ctx context.Context, q url.Values) ([]FootballLeagueDTO, error) {
	key := "fb:leagues?" + q.Encode()
	return cacheAside(ctx, fs.cache, key, fs.ttl, func(ctx context.Context) ([]FootballLeagueDTO, error) {
		leagues, err := fs.client.Leagues(ctx, q)
		if err != nil {
			return nil, err
		}
		out := make([]FootballLeagueDTO, 0, len(leagues))
		for _, l := range leagues {
			out = append(out, FootballLeagueDTO{ID: l.ID, Name: l.Name, ImagePath: l.ImagePath})
		}
		return out, nil
	})
}

// ---- mapping ----

func mapFootballMatches(fx []football.Fixture) []FootballMatchDTO {
	out := make([]FootballMatchDTO, 0, len(fx))
	for _, f := range fx {
		out = append(out, footballMatch(f))
	}
	return out
}

func footballMatch(f football.Fixture) FootballMatchDTO {
	m := FootballMatchDTO{
		ID:         f.ID,
		Name:       f.Name,
		StartingAt: f.StartingAt,
		LeagueID:   f.LeagueID,
		SeasonID:   f.SeasonID,
	}
	if f.ResultInfo != nil {
		m.ResultInfo = *f.ResultInfo
	}
	if f.State != nil {
		m.Status = f.State.Name
		m.StatusShort = f.State.State
	}
	for _, p := range f.Participants {
		switch p.Meta.Location {
		case "home":
			m.HomeTeam = p.Name
		case "away":
			m.AwayTeam = p.Name
		}
	}
	// Use the CURRENT scoreline for the headline goals.
	for _, s := range f.Scores {
		if s.Description != "CURRENT" {
			continue
		}
		goals := s.Score.Goals
		switch s.Score.Participant {
		case "home":
			m.HomeGoals = &goals
		case "away":
			m.AwayGoals = &goals
		}
	}
	return m
}

func footballStanding(s football.Standing) FootballStandingDTO {
	d := FootballStandingDTO{Position: s.Position, Points: s.Points, TeamID: s.ParticipantID}
	if s.Participant != nil {
		d.Team = s.Participant.Name
		if d.TeamID == 0 {
			d.TeamID = s.Participant.ID
		}
	}
	for _, det := range s.Details {
		v := detailInt(det.Value)
		switch det.TypeID {
		case ftTypePlayed:
			d.Played = v
		case ftTypeWon:
			d.Won = v
		case ftTypeDraw:
			d.Draw = v
		case ftTypeLost:
			d.Lost = v
		case ftTypeGoalsFor:
			d.GoalsFor = v
		case ftTypeGoalsAgainst:
			d.GoalsAgainst = v
		}
	}
	d.GoalDiff = d.GoalsFor - d.GoalsAgainst
	return d
}

func detailInt(n json.Number) int {
	i, _ := n.Int64()
	return int(i)
}

// ---- cache-aside helpers (cache-backed; independent of the cricket Service) ----

func cacheAside[T any](ctx context.Context, c cache.Cache, key string, ttl time.Duration, load func(context.Context) (T, error)) (T, error) {
	var zero T
	if raw, ok := c.Get(key); ok {
		var v T
		if err := json.Unmarshal(raw, &v); err == nil {
			return v, nil
		}
	}
	v, err := load(ctx)
	if err != nil {
		return zero, err
	}
	if raw, err := json.Marshal(v); err == nil {
		c.Set(key, raw, ttl)
	}
	return v, nil
}

func cacheAsidePtr[T any](ctx context.Context, c cache.Cache, key string, ttl time.Duration, load func(context.Context) (*T, error)) (*T, error) {
	if raw, ok := c.Get(key); ok {
		var v T
		if err := json.Unmarshal(raw, &v); err == nil {
			return &v, nil
		}
	}
	v, err := load(ctx)
	if err != nil {
		return nil, err
	}
	if v != nil {
		if raw, err := json.Marshal(v); err == nil {
			c.Set(key, raw, ttl)
		}
	}
	return v, nil
}
