// Package httpapi wires the HTTP routes, handlers, and middleware.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/bgmaster/sports-sdv/internal/flights"
	"github.com/bgmaster/sports-sdv/internal/sports"
	"github.com/bgmaster/sports-sdv/internal/webui"
)

// NewRouter builds the http.Handler for the service. It uses the stdlib
// ServeMux method+pattern routing added in Go 1.22.
func NewRouter(cricket sports.CricketAPI, football sports.FootballAPI, flight *flights.Service, log *slog.Logger) http.Handler {
	h := &Handlers{svc: cricket}
	fb := &FootballHandlers{svc: football}
	fl := &FlightHandlers{svc: flight}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)

	// Cricket (unchanged paths).
	mux.HandleFunc("GET /api/v1/livescores", h.livescores)
	mux.HandleFunc("GET /api/v1/matches", h.matches)
	mux.HandleFunc("GET /api/v1/matches/{id}/scorecard", h.scorecard)
	mux.HandleFunc("GET /api/v1/standings", h.standings)
	mux.HandleFunc("GET /api/v1/rankings", h.rankings)
	mux.HandleFunc("GET /api/v1/leagues", h.leagues)

	// Football (namespaced).
	mux.HandleFunc("GET /api/v1/football/livescores", fb.livescores)
	mux.HandleFunc("GET /api/v1/football/matches", fb.matches)
	mux.HandleFunc("GET /api/v1/football/matches/{id}", fb.match)
	mux.HandleFunc("GET /api/v1/football/standings", fb.standings)
	mux.HandleFunc("GET /api/v1/football/leagues", fb.leagues)

	// Flights (namespaced).
	mux.HandleFunc("GET /api/v1/flights", fl.search)

	// Chatbot UI (catch-all; more specific API routes above take precedence).
	mux.Handle("GET /", webui.Handler())

	// Middleware chain: recoverer(logging(mux)).
	var handler http.Handler = mux
	handler = logging(log, handler)
	handler = recoverer(log, handler)
	return handler
}
