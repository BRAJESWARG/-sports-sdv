package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// LogBodyMax caps how many chars of the client response body are logged
// (0 = unlimited). Set from config at startup.
var LogBodyMax = 2000

// statusRecorder captures the response status and (for API calls) a truncated
// copy of the response body for logging.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	body    []byte
	capture bool
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.capture {
		if LogBodyMax <= 0 {
			r.body = append(r.body, b...) // unlimited
		} else if len(r.body) < LogBodyMax {
			n := LogBodyMax - len(r.body)
			if n > len(b) {
				n = len(b)
			}
			r.body = append(r.body, b[:n]...)
		}
	}
	return r.ResponseWriter.Write(b)
}

// logging logs one line per request: method, path, query, status, latency, and
// (for /api/ calls) the response body we send back to the client.
func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK, capture: strings.HasPrefix(r.URL.Path, "/api/")}
		next.ServeHTTP(rec, r)
		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"status", rec.status,
			"dur", time.Since(start).String(),
		}
		if rec.capture {
			attrs = append(attrs, "response", truncateBody(rec.body))
		}
		log.Info("request", attrs...)
	})
}

func truncateBody(b []byte) string {
	if LogBodyMax <= 0 || len(b) < LogBodyMax {
		return string(b)
	}
	return string(b) + fmt.Sprintf("…(+more, capped at %d)", LogBodyMax)
}

// recoverer turns panics into 500s instead of crashing the server.
func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Error("panic", "err", err, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
