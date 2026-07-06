package sports

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/bgmaster/sports-sdv/internal/cache"
	"github.com/bgmaster/sports-sdv/internal/footballdata"
)

// FootballDataService is the football-data.org-backed implementation of
// FootballAPI. It's kept as a switchable alternative to the API-Football service
// (select via FOOTBALL_PROVIDER=footballdata); its free tier includes WC 2026.
type FootballDataService struct {
	client  *footballdata.Client
	cache   cache.Cache
	ttl     time.Duration
	ttlLive time.Duration
}

// NewFootballData wires the football-data.org service.
func NewFootballData(client *footballdata.Client, c cache.Cache, ttl, ttlLive time.Duration) *FootballDataService {
	return &FootballDataService{client: client, cache: c, ttl: ttl, ttlLive: ttlLive}
}

func (fs *FootballDataService) Livescores(ctx context.Context) ([]FootballMatchDTO, error) {
	return cacheAside(ctx, fs.cache, "fbd:livescores", fs.ttlLive, func(ctx context.Context) ([]FootballMatchDTO, error) {
		ms, err := fs.client.Livescores(ctx)
		if err != nil {
			return nil, err
		}
		return fdMapMatches(ms), nil
	})
}

func (fs *FootballDataService) Matches(ctx context.Context, q url.Values) ([]FootballMatchDTO, error) {
	date, from, to, comp := q.Get("date"), q.Get("from"), q.Get("to"), q.Get("competition")
	key := "fbd:matches?" + q.Encode()
	explicit := date != "" || (from != "" && to != "")
	return cacheAside(ctx, fs.cache, key, fs.ttl, func(ctx context.Context) ([]FootballMatchDTO, error) {
		var f0, t0 string
		switch {
		case date != "":
			f0, t0 = date, date
		case from != "" && to != "":
			f0, t0 = from, to
		default:
			now := time.Now().UTC()
			f0, t0 = now.Format("2006-01-02"), now.AddDate(0, 0, 7).Format("2006-01-02")
		}
		var ms []footballdata.Match
		var err error
		if comp != "" {
			ms, err = fs.client.MatchesByCompetition(ctx, comp, f0, t0)
		} else {
			ms, err = fs.client.MatchesBetween(ctx, f0, t0)
		}
		if err != nil {
			return nil, err
		}
		out := fdMapMatches(ms)
		// The competition endpoint returns the whole matchday, not a strict date
		// range — so enforce the requested window when the user asked for a date.
		if explicit {
			out = filterByDateWindow(out, f0, t0)
		}
		return out, nil
	})
}

func (fs *FootballDataService) Match(ctx context.Context, id int64) (*FootballMatchDTO, error) {
	key := fmt.Sprintf("fbd:match?id=%d", id)
	return cacheAsidePtr(ctx, fs.cache, key, fs.ttlLive, func(ctx context.Context) (*FootballMatchDTO, error) {
		mm, err := fs.client.Match(ctx, id)
		if err != nil {
			return nil, err
		}
		m := fdMatch(*mm)
		return &m, nil
	})
}

func (fs *FootballDataService) Standings(ctx context.Context, competition string) ([]FootballStandingDTO, error) {
	key := "fbd:standings?comp=" + competition
	return cacheAside(ctx, fs.cache, key, fs.ttl, func(ctx context.Context) ([]FootballStandingDTO, error) {
		resp, err := fs.client.Standings(ctx, competition)
		if err != nil {
			return nil, err
		}
		var out []FootballStandingDTO
		for _, g := range resp.Standings {
			if g.Type != "" && g.Type != "TOTAL" {
				continue
			}
			for _, r := range g.Table {
				out = append(out, FootballStandingDTO{
					Position: r.Position, Team: fdTeamLabel(r.Team), TeamID: r.Team.ID,
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

func (fs *FootballDataService) Leagues(ctx context.Context, _ url.Values) ([]FootballLeagueDTO, error) {
	return cacheAside(ctx, fs.cache, "fbd:leagues", fs.ttl, func(ctx context.Context) ([]FootballLeagueDTO, error) {
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

// ---- football-data.org mapping ----

func fdMapMatches(ms []footballdata.Match) []FootballMatchDTO {
	out := make([]FootballMatchDTO, 0, len(ms))
	for _, m := range ms {
		out = append(out, fdMatch(m))
	}
	return out
}

func fdMatch(m footballdata.Match) FootballMatchDTO {
	home, away := fdTeamLabel(m.HomeTeam), fdTeamLabel(m.AwayTeam)
	d := FootballMatchDTO{
		ID:          m.ID,
		Name:        home + " vs " + away,
		Status:      fdStatus(m.Status),
		StatusShort: m.Status,
		Live:        fdLive(m.Status),
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

func fdTeamLabel(t footballdata.TeamRef) string {
	if t.ShortName != "" {
		return t.ShortName
	}
	return t.Name
}

func fdLive(status string) bool {
	switch status {
	case "IN_PLAY", "PAUSED", "LIVE":
		return true
	}
	return false
}

func fdStatus(status string) string {
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
