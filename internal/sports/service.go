// Package sports is the business layer. It orchestrates the SportMonks Cricket
// client and the cache, and maps upstream types to your own DTOs.
//
// Flow for every read: build a cache key -> return cached DTOs on hit ->
// otherwise call upstream, map to DTOs, cache, and return.
package sports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/bgmaster/sports-sdv/internal/cache"
	"github.com/bgmaster/sports-sdv/internal/sportmonks"
)

// Service exposes the operations your HTTP handlers call.
type Service struct {
	client  *sportmonks.Client
	cache   cache.Cache
	ttl     time.Duration
	ttlLive time.Duration
}

// New wires the service.
func New(client *sportmonks.Client, c cache.Cache, ttl, ttlLive time.Duration) *Service {
	return &Service{client: client, cache: c, ttl: ttl, ttlLive: ttlLive}
}

// getCached is a generic cache-aside helper: return the decoded value on hit,
// otherwise run load(), cache the marshaled result, and return it.
func getCached[T any](ctx context.Context, s *Service, key string, ttl time.Duration, load func(context.Context) (T, error)) (T, error) {
	var zero T
	if raw, ok := s.cache.Get(key); ok {
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
		s.cache.Set(key, raw, ttl)
	}
	return v, nil
}

func teamName(d *sportmonks.Data[sportmonks.Team]) string {
	if d == nil {
		return ""
	}
	return d.Data.Name
}

// matchFromFixture maps an upstream fixture to a MatchDTO, resolving team names
// and innings totals from the localteam/visitorteam/runs includes.
func matchFromFixture(f sportmonks.Fixture) MatchDTO {
	m := MatchDTO{
		ID:          f.ID,
		Type:        f.Type,
		Status:      f.Status,
		Live:        f.Live,
		StartingAt:  f.StartingAt,
		LeagueID:    f.LeagueID,
		SeasonID:    f.SeasonID,
		Round:       f.Round,
		Note:        f.Note,
		LocalTeam:   teamName(f.LocalTeam),
		VisitorTeam: teamName(f.VisitorTeam),
	}
	names := teamNameMap(f)
	if f.Runs != nil {
		for _, r := range f.Runs.Data {
			m.Innings = append(m.Innings, InningsDTO{
				Team:    names[r.TeamID],
				Runs:    r.Score,
				Wickets: r.Wickets,
				Overs:   r.Overs,
			})
		}
	}
	return m
}

// teamNameMap builds a team-id -> name lookup from the localteam/visitorteam includes.
func teamNameMap(f sportmonks.Fixture) map[int]string {
	names := map[int]string{}
	if f.LocalTeam != nil {
		names[f.LocalTeam.Data.ID] = f.LocalTeam.Data.Name
	}
	if f.VisitorTeam != nil {
		names[f.VisitorTeam.Data.ID] = f.VisitorTeam.Data.Name
	}
	return names
}

// Livescores returns matches in play / scheduled today. Short-TTL cached.
func (s *Service) Livescores(ctx context.Context, q url.Values) ([]MatchDTO, error) {
	key := "livescores?" + q.Encode()
	return getCached(ctx, s, key, s.ttlLive, func(ctx context.Context) ([]MatchDTO, error) {
		fx, err := s.client.Livescores(ctx, q)
		if err != nil {
			return nil, err
		}
		return mapMatches(fx), nil
	})
}

// Matches returns fixtures (schedule & results). If no date/season/league
// filter is supplied, it defaults to the next 7 days so it never dumps the
// entire catalogue. Live-heavy windows get the short TTL.
func (s *Service) Matches(ctx context.Context, q url.Values) ([]MatchDTO, error) {
	q = defaultDateWindow(q)
	ttl := s.ttl
	if includesToday(q) {
		ttl = s.ttlLive
	}
	key := "matches?" + q.Encode()
	return getCached(ctx, s, key, ttl, func(ctx context.Context) ([]MatchDTO, error) {
		fx, err := s.client.Fixtures(ctx, q)
		if err != nil {
			return nil, err
		}
		return mapMatches(fx), nil
	})
}

// Scorecard returns the detailed scorecard for a single match.
func (s *Service) Scorecard(ctx context.Context, id int) (*ScorecardDTO, error) {
	key := fmt.Sprintf("scorecard?id=%d", id)
	return getCachedPtr(ctx, s, key, s.ttlLive, func(ctx context.Context) (*ScorecardDTO, error) {
		f, err := s.client.Fixture(ctx, id)
		if err != nil {
			return nil, err
		}
		return scorecardFromFixture(*f), nil
	})
}

// Standings returns the season standings table, sorted by position.
func (s *Service) Standings(ctx context.Context, seasonID int) ([]StandingDTO, error) {
	key := fmt.Sprintf("standings?season=%d", seasonID)
	return getCached(ctx, s, key, s.ttl, func(ctx context.Context) ([]StandingDTO, error) {
		rows, err := s.client.StandingsBySeason(ctx, seasonID)
		if err != nil {
			return nil, err
		}
		out := make([]StandingDTO, 0, len(rows))
		for _, r := range rows {
			out = append(out, StandingDTO{
				Position: r.Position,
				Team:     teamName(r.Team),
				TeamID:   r.TeamID,
				Points:   r.Points,
				Won:      r.Won,
				Lost:     r.Lost,
				Draw:     r.Draw,
				NoResult: r.NoResult,
			})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
		return out, nil
	})
}

// Rankings returns ICC team rankings. Query filters (tournament_type, gender)
// are passed through by the handler.
func (s *Service) Rankings(ctx context.Context, q url.Values) ([]RankingDTO, error) {
	key := "rankings?" + q.Encode()
	return getCached(ctx, s, key, s.ttl, func(ctx context.Context) ([]RankingDTO, error) {
		tables, err := s.client.TeamRankings(ctx, q)
		if err != nil {
			return nil, err
		}
		out := make([]RankingDTO, 0, len(tables))
		for _, t := range tables {
			r := RankingDTO{Type: t.Type, Gender: t.Gender}
			if t.Team != nil {
				for _, rt := range t.Team.Data {
					r.Teams = append(r.Teams, RankedTeamDTO{
						Position: rt.Position,
						Team:     rt.Name,
						Matches:  rt.Ranking.Matches,
						Rating:   rt.Ranking.Rating,
						Points:   rt.Ranking.Points,
					})
				}
				sort.Slice(r.Teams, func(i, j int) bool { return r.Teams[i].Position < r.Teams[j].Position })
			}
			out = append(out, r)
		}
		return out, nil
	})
}

// Leagues returns competitions.
func (s *Service) Leagues(ctx context.Context, q url.Values) ([]LeagueDTO, error) {
	key := "leagues?" + q.Encode()
	return getCached(ctx, s, key, s.ttl, func(ctx context.Context) ([]LeagueDTO, error) {
		leagues, err := s.client.Leagues(ctx, q)
		if err != nil {
			return nil, err
		}
		out := make([]LeagueDTO, 0, len(leagues))
		for _, l := range leagues {
			out = append(out, LeagueDTO{ID: l.ID, Name: l.Name, Code: l.Code, Type: l.Type})
		}
		return out, nil
	})
}

// ---- helpers ----

func mapMatches(fx []sportmonks.Fixture) []MatchDTO {
	out := make([]MatchDTO, 0, len(fx))
	for _, f := range fx {
		out = append(out, matchFromFixture(f))
	}
	return out
}

func scorecardFromFixture(f sportmonks.Fixture) *ScorecardDTO {
	sc := &ScorecardDTO{
		MatchID:     f.ID,
		Type:        f.Type,
		Status:      f.Status,
		Note:        f.Note,
		LocalTeam:   teamName(f.LocalTeam),
		VisitorTeam: teamName(f.VisitorTeam),
	}
	names := teamNameMap(f)
	if f.Runs != nil {
		for _, r := range f.Runs.Data {
			sc.Innings = append(sc.Innings, InningsDTO{Team: names[r.TeamID], Runs: r.Score, Wickets: r.Wickets, Overs: r.Overs})
		}
	}
	if f.Batting != nil {
		for _, b := range f.Batting.Data {
			name := ""
			if b.Batsman != nil {
				name = b.Batsman.Data.Fullname
			}
			sc.Batting = append(sc.Batting, BattingDTO{
				Player: name, TeamID: b.TeamID, Runs: b.Score,
				Balls: b.Ball, Fours: b.FourX, Sixes: b.SixX, StrikeRate: b.Rate,
			})
		}
	}
	if f.Bowling != nil {
		for _, b := range f.Bowling.Data {
			name := ""
			if b.Bowler != nil {
				name = b.Bowler.Data.Fullname
			}
			sc.Bowling = append(sc.Bowling, BowlingDTO{
				Player: name, TeamID: b.TeamID, Overs: b.Overs,
				Runs: b.Runs, Wickets: b.Wickets, Economy: b.Rate,
			})
		}
	}
	return sc
}

// getCachedPtr is getCached for pointer results (nil-safe marshal/unmarshal).
func getCachedPtr[T any](ctx context.Context, s *Service, key string, ttl time.Duration, load func(context.Context) (*T, error)) (*T, error) {
	if raw, ok := s.cache.Get(key); ok {
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
			s.cache.Set(key, raw, ttl)
		}
	}
	return v, nil
}

// defaultDateWindow injects a today..today+7d filter when the caller supplied
// no narrowing filter, so /matches never returns the whole catalogue.
func defaultDateWindow(q url.Values) url.Values {
	if q.Get("filter[starts_between]") != "" ||
		q.Get("filter[league_id]") != "" ||
		q.Get("filter[season_id]") != "" ||
		q.Get("filter[localteam_id]") != "" ||
		q.Get("filter[visitorteam_id]") != "" {
		return q
	}
	now := time.Now().UTC()
	from := now.Format("2006-01-02")
	to := now.AddDate(0, 0, 7).Format("2006-01-02")
	q.Set("filter[starts_between]", from+","+to)
	return q
}

func includesToday(q url.Values) bool {
	today := time.Now().UTC().Format("2006-01-02")
	between := q.Get("filter[starts_between]")
	if between == "" {
		return false
	}
	// The "from" date is the first 10 chars (YYYY-MM-DD); compares lexically.
	from := between[:min(len(between), 10)]
	return from <= today
}
