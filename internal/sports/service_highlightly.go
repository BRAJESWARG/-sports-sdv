package sports

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bgmaster/sports-sdv/internal/cache"
	"github.com/bgmaster/sports-sdv/internal/highlightly"
)

// CricketAPI is the cricket operation set the HTTP layer depends on. Both the
// legacy SportMonks service (*Service) and the Highlightly service implement it,
// so the provider can be swapped at wiring time.
type CricketAPI interface {
	Livescores(ctx context.Context, q url.Values) ([]MatchDTO, error)
	Matches(ctx context.Context, q url.Values) ([]MatchDTO, error)
	Scorecard(ctx context.Context, id int) (*ScorecardDTO, error)
	Standings(ctx context.Context, seasonID int) ([]StandingDTO, error)
	Rankings(ctx context.Context, q url.Values) ([]RankingDTO, error)
	Leagues(ctx context.Context, q url.Values) ([]LeagueDTO, error)
}

// hlMaxDays caps how many single-day upstream calls one ranged query makes
// (Highlightly's /matches is per-day, so a wide window would otherwise fan out).
const hlMaxDays = 14

// ---------------- Cricket ----------------

// HighlightlyCricketService implements CricketAPI against Highlightly cricket.
type HighlightlyCricketService struct {
	client  *highlightly.Client
	cache   cache.Cache
	ttl     time.Duration
	ttlLive time.Duration
}

// NewHighlightlyCricket wires the Highlightly cricket service.
func NewHighlightlyCricket(client *highlightly.Client, c cache.Cache, ttl, ttlLive time.Duration) *HighlightlyCricketService {
	return &HighlightlyCricketService{client: client, cache: c, ttl: ttl, ttlLive: ttlLive}
}

func (s *HighlightlyCricketService) fetchDay(ctx context.Context, date string, ttl time.Duration) ([]MatchDTO, error) {
	return cacheAside(ctx, s.cache, "hl:cricket:"+date, ttl, func(ctx context.Context) ([]MatchDTO, error) {
		ms, err := s.client.CricketMatches(ctx, date)
		if err != nil {
			return nil, err
		}
		out := make([]MatchDTO, 0, len(ms))
		for _, m := range ms {
			out = append(out, mapHLCricket(m))
		}
		return out, nil
	})
}

// Livescores returns cricket matches in play today (IST).
func (s *HighlightlyCricketService) Livescores(ctx context.Context, _ url.Values) ([]MatchDTO, error) {
	day, err := s.fetchDay(ctx, todayISTDate(), s.ttlLive)
	if err != nil {
		return nil, err
	}
	out := make([]MatchDTO, 0, len(day))
	for _, m := range day {
		if m.Live {
			out = append(out, m)
		}
	}
	return out, nil
}

// Matches returns cricket fixtures/results across the requested date window.
func (s *HighlightlyCricketService) Matches(ctx context.Context, q url.Values) ([]MatchDTO, error) {
	var out []MatchDTO
	for _, date := range cricketDates(q) {
		day, err := s.fetchDay(ctx, date, s.ttl)
		if err != nil {
			return nil, err
		}
		out = append(out, day...)
	}
	return out, nil
}

// Scorecard/Standings/Rankings/Leagues are not sourced from the Highlightly
// /matches endpoint used here; they degrade to empty rather than erroring.
func (s *HighlightlyCricketService) Scorecard(context.Context, int) (*ScorecardDTO, error) {
	return nil, nil
}
func (s *HighlightlyCricketService) Standings(context.Context, int) ([]StandingDTO, error) {
	return []StandingDTO{}, nil
}
func (s *HighlightlyCricketService) Rankings(context.Context, url.Values) ([]RankingDTO, error) {
	return []RankingDTO{}, nil
}
func (s *HighlightlyCricketService) Leagues(context.Context, url.Values) ([]LeagueDTO, error) {
	return []LeagueDTO{}, nil
}

func mapHLCricket(m highlightly.CricketMatch) MatchDTO {
	id, _ := strconv.Atoi(m.ID)
	lid, _ := strconv.Atoi(m.League.ID)
	d := MatchDTO{
		ID:          id,
		Type:        m.Format,
		Status:      m.State.Description,
		Live:        isCricketLive(m.State.Description),
		StartingAt:  m.StartTime,
		LeagueID:    lid,
		SeasonID:    m.League.Season,
		League:      m.League.Name,
		Note:        m.State.Report,
		LocalTeam:   m.HomeTeam.Name,
		VisitorTeam: m.AwayTeam.Name,
	}
	if inn, ok := parseCricketInnings(m.HomeTeam.Name, m.State.Teams.Home); ok {
		d.Innings = append(d.Innings, inn)
	}
	if inn, ok := parseCricketInnings(m.AwayTeam.Name, m.State.Teams.Away); ok {
		d.Innings = append(d.Innings, inn)
	}
	return d
}

// ---------------- Football ----------------

// HighlightlyFootballService implements FootballAPI against Highlightly football.
type HighlightlyFootballService struct {
	client  *highlightly.Client
	cache   cache.Cache
	ttl     time.Duration
	ttlLive time.Duration
}

// NewHighlightlyFootball wires the Highlightly football service.
func NewHighlightlyFootball(client *highlightly.Client, c cache.Cache, ttl, ttlLive time.Duration) *HighlightlyFootballService {
	return &HighlightlyFootballService{client: client, cache: c, ttl: ttl, ttlLive: ttlLive}
}

func (s *HighlightlyFootballService) fetchDay(ctx context.Context, date string, ttl time.Duration) ([]FootballMatchDTO, error) {
	return cacheAside(ctx, s.cache, "hl:football:"+date, ttl, func(ctx context.Context) ([]FootballMatchDTO, error) {
		ms, err := s.client.FootballMatches(ctx, date)
		if err != nil {
			return nil, err
		}
		out := make([]FootballMatchDTO, 0, len(ms))
		for _, m := range ms {
			out = append(out, mapHLFootball(m))
		}
		return out, nil
	})
}

// Livescores returns football matches in play today (IST).
func (s *HighlightlyFootballService) Livescores(ctx context.Context) ([]FootballMatchDTO, error) {
	day, err := s.fetchDay(ctx, todayISTDate(), s.ttlLive)
	if err != nil {
		return nil, err
	}
	out := make([]FootballMatchDTO, 0, len(day))
	for _, m := range day {
		if m.Live {
			out = append(out, m)
		}
	}
	return out, nil
}

// Matches returns football fixtures/results across the requested date window.
// The competition filter is applied client-side by the chatbot, so it's ignored here.
func (s *HighlightlyFootballService) Matches(ctx context.Context, q url.Values) ([]FootballMatchDTO, error) {
	var out []FootballMatchDTO
	for _, date := range footballDates(q) {
		day, err := s.fetchDay(ctx, date, s.ttl)
		if err != nil {
			return nil, err
		}
		out = append(out, day...)
	}
	return out, nil
}

// Match/Standings/Leagues aren't sourced from the /matches endpoint used here.
func (s *HighlightlyFootballService) Match(context.Context, int64) (*FootballMatchDTO, error) {
	return nil, nil
}
func (s *HighlightlyFootballService) Standings(context.Context, string) ([]FootballStandingDTO, error) {
	return []FootballStandingDTO{}, nil
}
func (s *HighlightlyFootballService) Leagues(context.Context, url.Values) ([]FootballLeagueDTO, error) {
	return []FootballLeagueDTO{}, nil
}

func mapHLFootball(m highlightly.FootballMatch) FootballMatchDTO {
	hg, ag := parseFootballScore(m.State.Score.Current)
	d := FootballMatchDTO{
		ID:          m.ID,
		Name:        m.HomeTeam.Name + " vs " + m.AwayTeam.Name,
		Status:      m.State.Description,
		StatusShort: m.State.Description,
		Live:        isFootballLive(m.State.Description),
		StartingAt:  m.Date,
		League:      m.League.Name,
		LeagueID:    m.League.ID,
		HomeTeam:    m.HomeTeam.Name,
		AwayTeam:    m.AwayTeam.Name,
		HomeGoals:   hg,
		AwayGoals:   ag,
	}
	if isFinished(m.State.Description) && hg != nil && ag != nil {
		d.ResultInfo = footballResult(m.HomeTeam.Name, m.AwayTeam.Name, *hg, *ag)
	}
	return d
}

// ---------------- shared helpers ----------------

// todayISTDate is today's calendar date in IST (matches how the UI computes days).
func todayISTDate() string { return time.Now().In(istZone).Format("2006-01-02") }

// cricketDates derives the day list from the cricket handler's filter, which is
// filter[starts_between]="from,exclusiveEnd" (the end is midnight-exclusive).
func cricketDates(q url.Values) []string {
	if sb := q.Get("filter[starts_between]"); sb != "" {
		if a, b, ok := strings.Cut(sb, ","); ok {
			return datesInRange(a, b, true)
		}
	}
	if d := q.Get("date"); d != "" {
		return []string{d}
	}
	if f, t := q.Get("from"), q.Get("to"); f != "" && t != "" {
		return datesInRange(f, t, false)
	}
	return []string{todayISTDate()}
}

// footballDates derives the day list from the football handler's date/from/to.
func footballDates(q url.Values) []string {
	if d := q.Get("date"); d != "" {
		return []string{d}
	}
	if f, t := q.Get("from"), q.Get("to"); f != "" && t != "" {
		return datesInRange(f, t, false)
	}
	return []string{todayISTDate()}
}

// datesInRange lists YYYY-MM-DD dates from start to end (endExclusive drops the
// last day), capped at hlMaxDays to bound upstream fan-out.
func datesInRange(start, end string, endExclusive bool) []string {
	s, err1 := time.Parse("2006-01-02", start)
	e, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil {
		return []string{start}
	}
	var out []string
	for d := s; ; d = d.AddDate(0, 0, 1) {
		if endExclusive && !d.Before(e) {
			break
		}
		if !endExclusive && d.After(e) {
			break
		}
		out = append(out, d.Format("2006-01-02"))
		if len(out) >= hlMaxDays {
			break
		}
	}
	if len(out) == 0 {
		out = append(out, start)
	}
	return out
}

func parseFootballScore(s string) (*int, *int) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return nil, nil
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(a))
	v, err2 := strconv.Atoi(strings.TrimSpace(b))
	if err1 != nil || err2 != nil {
		return nil, nil
	}
	return &h, &v
}

func parseCricketInnings(team string, in highlightly.CricketInnings) (InningsDTO, bool) {
	if strings.TrimSpace(in.Score) == "" {
		return InningsDTO{}, false
	}
	runsStr, wktsStr, hasWkts := strings.Cut(in.Score, "/")
	runs, _ := strconv.Atoi(strings.TrimSpace(runsStr))
	wkts := 0
	if hasWkts {
		wkts, _ = strconv.Atoi(strings.TrimSpace(wktsStr))
	}
	return InningsDTO{Team: team, Runs: runs, Wickets: wkts, Overs: parseOvers(in.Info)}, true
}

// parseOvers reads the leading number of an info string like "20 ov, T:217".
func parseOvers(info string) float64 {
	f := strings.Fields(strings.TrimSpace(info))
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

func footballResult(home, away string, hg, ag int) string {
	switch {
	case hg > ag:
		return home + " won"
	case ag > hg:
		return away + " won"
	default:
		return "Draw"
	}
}

func isFinished(desc string) bool {
	return strings.EqualFold(strings.TrimSpace(desc), "finished")
}

// isFootballLive treats known in-play descriptions as live.
func isFootballLive(desc string) bool {
	switch strings.ToLower(strings.TrimSpace(desc)) {
	case "first half", "second half", "halftime", "half time", "extra time",
		"penalties", "penalty shootout", "break time", "in play", "live":
		return true
	}
	return false
}

// isCricketLive treats anything that isn't terminal/not-started as live.
func isCricketLive(desc string) bool {
	switch strings.ToLower(strings.TrimSpace(desc)) {
	case "", "finished", "not started", "abandoned", "cancelled", "canceled",
		"postponed", "no result":
		return false
	}
	return true
}
