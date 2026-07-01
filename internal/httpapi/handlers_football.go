package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/bgmaster/sports-sdv/internal/sports"
)

// FootballHandlers serves the /api/v1/football/* routes.
type FootballHandlers struct {
	svc *sports.FootballService
}

// GET /api/v1/football/livescores
func (h *FootballHandlers) livescores(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.Livescores(r.Context())
	if err != nil {
		mapUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(data), "data": data})
}

// GET /api/v1/football/matches?date=&from=&to=&season=&league=
func (h *FootballHandlers) matches(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	for _, k := range []string{"date", "from", "to", "season", "league"} {
		if v := r.URL.Query().Get(k); v != "" {
			q.Set(k, v)
		}
	}
	data, err := h.svc.Matches(r.Context(), q)
	if err != nil {
		mapUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(data), "data": data})
}

// GET /api/v1/football/matches/{id}
func (h *FootballHandlers) match(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid match id")
		return
	}
	data, err := h.svc.Match(r.Context(), id)
	if err != nil {
		mapUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// GET /api/v1/football/standings?season=
func (h *FootballHandlers) standings(w http.ResponseWriter, r *http.Request) {
	seasonID, err := strconv.ParseInt(r.URL.Query().Get("season"), 10, 64)
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

// GET /api/v1/football/leagues
func (h *FootballHandlers) leagues(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.Leagues(r.Context(), url.Values{})
	if err != nil {
		mapUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(data), "data": data})
}
