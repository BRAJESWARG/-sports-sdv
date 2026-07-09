package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/bgmaster/sports-sdv/internal/aviationstack"
	"github.com/bgmaster/sports-sdv/internal/football"
	"github.com/bgmaster/sports-sdv/internal/sportmonks"
	"github.com/bgmaster/sports-sdv/internal/sports"
)

// Handlers holds dependencies for the HTTP handlers.
type Handlers struct {
	svc *sports.Service
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// mapUpstreamError translates upstream failures into appropriate HTTP codes.
func mapUpstreamError(w http.ResponseWriter, err error) {
	// We reached upstream but it was unhappy (auth, quota, bad params, proxy).
	var crickErr *sportmonks.APIError
	if errors.As(err, &crickErr) {
		writeError(w, http.StatusBadGateway, "upstream error: "+crickErr.Message)
		return
	}
	var fbErr *football.APIError
	if errors.As(err, &fbErr) {
		writeError(w, http.StatusBadGateway, "upstream error: "+fbErr.Message)
		return
	}
	var flErr *aviationstack.APIError
	if errors.As(err, &flErr) {
		writeError(w, http.StatusBadGateway, "upstream error: "+flErr.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func (h *Handlers) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /api/v1/livescores
func (h *Handlers) livescores(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.Livescores(r.Context(), url.Values{})
	if err != nil {
		mapUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(data), "data": data})
}

// GET /api/v1/matches?date=&from=&to=&league=&season=&team=
func (h *Handlers) matches(w http.ResponseWriter, r *http.Request) {
	q := buildFixtureFilters(r.URL.Query())
	data, err := h.svc.Matches(r.Context(), q)
	if err != nil {
		mapUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(data), "data": data})
}

// GET /api/v1/matches/{id}/scorecard
func (h *Handlers) scorecard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}
	data, err := h.svc.Scorecard(r.Context(), id)
	if err != nil {
		mapUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// GET /api/v1/standings?season=
func (h *Handlers) standings(w http.ResponseWriter, r *http.Request) {
	season := r.URL.Query().Get("season")
	seasonID, err := strconv.Atoi(season)
	if err != nil || seasonID <= 0 {
		writeError(w, http.StatusBadRequest, "query param 'season' (numeric season id) is required")
		return
	}
	data, err := h.svc.Standings(r.Context(), seasonID)
	if err != nil {
		mapUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(data), "data": data})
}

// GET /api/v1/rankings?type=TEST|ODI|T20I&gender=men|women
func (h *Handlers) rankings(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	if t := r.URL.Query().Get("type"); t != "" {
		q.Set("filter[type]", t)
	}
	if g := r.URL.Query().Get("gender"); g != "" {
		q.Set("filter[gender]", g)
	}
	data, err := h.svc.Rankings(r.Context(), q)
	if err != nil {
		mapUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(data), "data": data})
}

// GET /api/v1/leagues
func (h *Handlers) leagues(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.Leagues(r.Context(), url.Values{})
	if err != nil {
		mapUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(data), "data": data})
}

// buildFixtureFilters translates friendly query params into SportMonks
// filter[...] syntax, forwarding only known keys.
func buildFixtureFilters(src url.Values) url.Values {
	out := url.Values{}
	// SportMonks treats the end of starts_between as midnight, so a same-day
	// range (from==to) would exclude that day's fixtures. Extend the end by one
	// day so the whole `to` day is covered.
	if date := src.Get("date"); date != "" {
		out.Set("filter[starts_between]", date+","+nextDay(date))
	} else if from, to := src.Get("from"), src.Get("to"); from != "" && to != "" {
		out.Set("filter[starts_between]", from+","+nextDay(to))
	}
	if v := src.Get("league"); v != "" {
		out.Set("filter[league_id]", v)
	}
	if v := src.Get("season"); v != "" {
		out.Set("filter[season_id]", v)
	}
	if v := src.Get("team"); v != "" {
		out.Set("filter[localteam_id]", v)
	}
	return out
}

// nextDay returns the day after a YYYY-MM-DD date (unchanged if unparseable).
func nextDay(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}
