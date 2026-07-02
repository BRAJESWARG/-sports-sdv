package sports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/bgmaster/sports-sdv/internal/cache"
	"github.com/bgmaster/sports-sdv/internal/football"
)

// FootballService orchestrates the API-Football client + cache and maps upstream
// types to football DTOs.
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

// fbLeagueID maps the UI's competition identifiers to API-Football league ids.
var fbLeagueID = map[string]int{
	"WC": 1, "CL": 2, "PL": 39, "PD": 140, "BL1": 78, "SA": 135,
	"FL1": 61, "DED": 88, "PPL": 94, "EC": 4, "ELC": 40, "BSA": 71,
}

func leagueIDFor(comp string) int { return fbLeagueID[strings.ToUpper(comp)] }

// currentSeason is the API-Football season year (a domestic 2026/27 season and
// the 2026 World Cup are both season 2026).
func currentSeason() int { return time.Now().UTC().Year() }

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

// Matches returns matches. A competition code scopes to that league+season
// (required by API-Football for date ranges); otherwise it returns a single
// day's fixtures (default today).
func (fs *FootballService) Matches(ctx context.Context, q url.Values) ([]FootballMatchDTO, error) {
	date, from, to, comp := q.Get("date"), q.Get("from"), q.Get("to"), q.Get("competition")
	key := "fb:matches?" + q.Encode()
	return cacheAside(ctx, fs.cache, key, fs.ttl, func(ctx context.Context) ([]FootballMatchDTO, error) {
		var fx []football.Fixture
		var err error
		if comp != "" {
			league := leagueIDFor(comp)
			if league == 0 {
				return []FootballMatchDTO{}, nil // unknown competition -> no football matches
			}
			f0, t0 := from, to
			if date != "" {
				f0, t0 = date, date
			}
			if f0 == "" || t0 == "" { // default: current fortnight of the tournament
				now := time.Now().UTC()
				f0, t0 = now.Format("2006-01-02"), now.AddDate(0, 0, 14).Format("2006-01-02")
			}
			fx, err = fs.client.FixturesByLeague(ctx, league, currentSeason(), f0, t0)
		} else {
			day := firstNonEmpty(date, from, time.Now().UTC().Format("2006-01-02"))
			fx, err = fs.client.FixturesByDate(ctx, day)
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

// Standings returns a competition's league table (by code, e.g. "PL"), sorted.
func (fs *FootballService) Standings(ctx context.Context, competition string) ([]FootballStandingDTO, error) {
	league := leagueIDFor(competition)
	season := currentSeason()
	key := fmt.Sprintf("fb:standings?league=%d&season=%d", league, season)
	return cacheAside(ctx, fs.cache, key, fs.ttl, func(ctx context.Context) ([]FootballStandingDTO, error) {
		if league == 0 {
			return []FootballStandingDTO{}, nil
		}
		resp, err := fs.client.Standings(ctx, league, season)
		if err != nil {
			return nil, err
		}
		var out []FootballStandingDTO
		for _, r := range resp {
			for _, group := range r.League.Standings {
				for _, row := range group {
					out = append(out, FootballStandingDTO{
						Position: row.Rank, Team: row.Team.Name, TeamID: int64(row.Team.ID),
						Points: row.Points, Played: row.All.Played, Won: row.All.Win,
						Draw: row.All.Draw, Lost: row.All.Lose, GoalsFor: row.All.Goals.For,
						GoalsAgainst: row.All.Goals.Against, GoalDiff: row.GoalsDiff,
					})
				}
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
		return out, nil
	})
}

// Leagues returns current leagues.
func (fs *FootballService) Leagues(ctx context.Context, _ url.Values) ([]FootballLeagueDTO, error) {
	return cacheAside(ctx, fs.cache, "fb:leagues", fs.ttl, func(ctx context.Context) ([]FootballLeagueDTO, error) {
		leagues, err := fs.client.Leagues(ctx, nil)
		if err != nil {
			return nil, err
		}
		out := make([]FootballLeagueDTO, 0, len(leagues))
		for _, l := range leagues {
			out = append(out, FootballLeagueDTO{ID: int64(l.League.ID), Name: l.League.Name, ImagePath: l.League.Logo})
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
	home, away := f.Teams.Home.Name, f.Teams.Away.Name
	d := FootballMatchDTO{
		ID:          int64(f.Fixture.ID),
		Name:        home + " vs " + away,
		Status:      f.Fixture.Status.Long,
		StatusShort: f.Fixture.Status.Short,
		Live:        footballLive(f.Fixture.Status.Short),
		StartingAt:  f.Fixture.Date,
		League:      f.League.Name,
		LeagueID:    int64(f.League.ID),
		HomeTeam:    home,
		AwayTeam:    away,
		HomeGoals:   f.Goals.Home,
		AwayGoals:   f.Goals.Away,
	}
	switch f.Fixture.Status.Short {
	case "FT", "AET", "PEN":
		switch {
		case f.Teams.Home.Winner != nil && *f.Teams.Home.Winner:
			d.ResultInfo = home + " won"
		case f.Teams.Away.Winner != nil && *f.Teams.Away.Winner:
			d.ResultInfo = away + " won"
		default:
			d.ResultInfo = "Draw"
		}
	}
	return d
}

// footballLive reports whether an API-Football status short code is in-play.
func footballLive(short string) bool {
	switch short {
	case "1H", "HT", "2H", "ET", "BT", "P", "LIVE", "INT":
		return true
	}
	return false
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
