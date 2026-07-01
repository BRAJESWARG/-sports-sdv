// Package httpapi wires the HTTP routes, handlers, and middleware.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/bgmaster/sports-sdv/internal/sports"
	"github.com/bgmaster/sports-sdv/internal/webui"
)

// NewRouter builds the http.Handler for the service. It uses the stdlib
// ServeMux method+pattern routing added in Go 1.22.
func NewRouter(cricket *sports.Service, football *sports.FootballService, cricketMock, footballMock bool, log *slog.Logger) http.Handler {
	h := &Handlers{svc: cricket}
	fb := &FootballHandlers{svc: football}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)

	// Mode info for the UI badge.
	mux.HandleFunc("GET /api/v1/meta", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"cricket":  map[string]string{"mode": mode(cricketMock)},
			"football": map[string]string{"mode": mode(footballMock)},
		})
	})

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

	// Chatbot UI (catch-all; more specific API routes above take precedence).
	mux.Handle("GET /", webui.Handler())

	// Middleware chain: recoverer(logging(mux)).
	var handler http.Handler = mux
	handler = logging(log, handler)
	handler = recoverer(log, handler)
	return handler
}

// mode maps a mock flag to a human-readable mode string for /api/v1/meta.
func mode(mock bool) string {
	if mock {
		return "mock"
	}
	return "live"
}
