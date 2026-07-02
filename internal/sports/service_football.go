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

// FootballService orchestrates the football-data.org client + cache and maps
// upstream types to football DTOs.
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

// Livescores returns matches in play. Short-TTL cached.
func (fs *FootballService) Livescores(ctx context.Context) ([]FootballMatchDTO, error) {
	return cacheAside(ctx, fs.cache, "fb:livescores", fs.ttlLive, func(ctx context.Context) ([]FootballMatchDTO, error) {
		ms, err := fs.client.Livescores(ctx)
		if err != nil {
			return nil, err
		}
		return mapFootballMatches(ms), nil
	})
}

// Matches returns matches (schedule & results). Honors date / from+to; with no
// filter it defaults to the next 7 days.
func (fs *FootballService) Matches(ctx context.Context, q url.Values) ([]FootballMatchDTO, error) {
	date, from, to := q.Get("date"), q.Get("from"), q.Get("to")
	key := "fb:matches?" + q.Encode()
	ttl := fs.ttl
	return cacheAside(ctx, fs.cache, key, ttl, func(ctx context.Context) ([]FootballMatchDTO, error) {
		var ms []football.Match
		var err error
		switch {
		case date != "":
			ms, err = fs.client.MatchesBetween(ctx, date, date)
		case from != "" && to != "":
			ms, err = fs.client.MatchesBetween(ctx, from, to)
		default:
			now := time.Now().UTC()
			ms, err = fs.client.MatchesBetween(ctx, now.Format("2006-01-02"), now.AddDate(0, 0, 7).Format("2006-01-02"))
		}
		if err != nil {
			return nil, err
		}
		return mapFootballMatches(ms), nil
	})
}

// Match returns a single match's detail.
func (fs *FootballService) Match(ctx context.Context, id int64) (*FootballMatchDTO, error) {
	key := fmt.Sprintf("fb:match?id=%d", id)
	return cacheAsidePtr(ctx, fs.cache, key, fs.ttlLive, func(ctx context.Context) (*FootballMatchDTO, error) {
		mm, err := fs.client.Match(ctx, id)
		if err != nil {
			return nil, err
		}
		m := footballMatch(*mm)
		return &m, nil
	})
}

// Standings returns a competition's league table (by code, e.g. "PL"), sorted.
func (fs *FootballService) Standings(ctx context.Context, competition string) ([]FootballStandingDTO, error) {
	key := "fb:standings?comp=" + competition
	return cacheAside(ctx, fs.cache, key, fs.ttl, func(ctx context.Context) ([]FootballStandingDTO, error) {
		resp, err := fs.client.Standings(ctx, competition)
		if err != nil {
			return nil, err
		}
		var out []FootballStandingDTO
		for _, g := range resp.Standings {
			if g.Type != "" && g.Type != "TOTAL" {
				continue // prefer the overall table
			}
			for _, r := range g.Table {
				out = append(out, FootballStandingDTO{
					Position: r.Position, Team: teamLabel(r.Team), TeamID: r.Team.ID,
					Points: r.Points, Played: r.PlayedGames, Won: r.Won, Draw: r.Draw,
					Lost: r.Lost, GoalsFor: r.GoalsFor, GoalsAgainst: r.GoalsAgainst,
					GoalDiff: r.GoalDifference,
				})
			}
			break
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
		return out, nil
	})
}

// Leagues returns the competitions the token can access.
func (fs *FootballService) Leagues(ctx context.Context, _ url.Values) ([]FootballLeagueDTO, error) {
	return cacheAside(ctx, fs.cache, "fb:leagues", fs.ttl, func(ctx context.Context) ([]FootballLeagueDTO, error) {
		comps, err := fs.client.Competitions(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]FootballLeagueDTO, 0, len(comps))
		for _, c := range comps {
			out = append(out, FootballLeagueDTO{ID: c.ID, Name: c.Name, ImagePath: c.Emblem})
		}
		return out, nil
	})
}

// ---- mapping ----

func mapFootballMatches(ms []football.Match) []FootballMatchDTO {
	out := make([]FootballMatchDTO, 0, len(ms))
	for _, m := range ms {
		out = append(out, footballMatch(m))
	}
	return out
}

func footballMatch(m football.Match) FootballMatchDTO {
	home, away := teamLabel(m.HomeTeam), teamLabel(m.AwayTeam)
	d := FootballMatchDTO{
		ID:          m.ID,
		Name:        home + " vs " + away,
		Status:      footballStatus(m.Status),
		StatusShort: m.Status,
		Live:        footballLive(m.Status),
		StartingAt:  m.UtcDate,
		League:      m.Competition.Name,
		LeagueID:    m.Competition.ID,
		HomeTeam:    home,
		AwayTeam:    away,
		HomeGoals:   m.Score.FullTime.Home,
		AwayGoals:   m.Score.FullTime.Away,
	}
	if m.Status == "FINISHED" {
		switch m.Score.Winner {
		case "HOME_TEAM":
			d.ResultInfo = home + " won"
		case "AWAY_TEAM":
			d.ResultInfo = away + " won"
		case "DRAW":
			d.ResultInfo = "Draw"
		}
	}
	return d
}

func teamLabel(t football.TeamRef) string {
	if t.ShortName != "" {
		return t.ShortName
	}
	return t.Name
}

func footballLive(status string) bool {
	switch status {
	case "IN_PLAY", "PAUSED", "LIVE":
		return true
	}
	return false
}

func footballStatus(status string) string {
	switch status {
	case "IN_PLAY":
		return "In Play"
	case "PAUSED":
		return "Half Time"
	case "FINISHED":
		return "Full Time"
	case "SCHEDULED", "TIMED":
		return "Scheduled"
	case "POSTPONED":
		return "Postponed"
	case "SUSPENDED":
		return "Suspended"
	case "CANCELLED":
		return "Cancelled"
	default:
		return status
	}
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
