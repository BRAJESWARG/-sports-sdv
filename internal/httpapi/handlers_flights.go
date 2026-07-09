package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/bgmaster/sports-sdv/internal/flights"
)

// FlightHandlers serves the /api/v1/flights route.
type FlightHandlers struct {
	svc *flights.Service
}

// friendlyToUpstream maps the API's friendly query params to AviationStack's.
var friendlyToUpstream = map[string]string{
	"from":    "dep_iata",     // departure airport IATA, e.g. DEL
	"to":      "arr_iata",     // arrival airport IATA, e.g. BOM
	"flight":  "flight_iata",  // flight number IATA, e.g. AI865
	"airline": "airline_iata", // airline IATA, e.g. AI
	"date":    "flight_date",  // YYYY-MM-DD
	"status":  "flight_status",
}

// upperParams are the code-like params we normalise to upper-case.
var upperParams = map[string]bool{"from": true, "to": true, "flight": true, "airline": true}

// GET /api/v1/flights?from=DEL&to=BOM&flight=AI865&airline=AI&date=2026-07-10&status=scheduled&limit=20
func (h *FlightHandlers) search(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	for friendly, upstream := range friendlyToUpstream {
		v := strings.TrimSpace(r.URL.Query().Get(friendly))
		if v == "" {
			continue
		}
		if upperParams[friendly] {
			v = strings.ToUpper(v)
		}
		q.Set(upstream, v)
	}
	limit := strings.TrimSpace(r.URL.Query().Get("limit"))
	if limit == "" {
		limit = "20"
	}
	q.Set("limit", limit)

	data, err := h.svc.Search(r.Context(), q)
	if err != nil {
		mapUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(data), "data": data})
}
