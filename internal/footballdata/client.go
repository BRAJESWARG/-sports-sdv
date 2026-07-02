// Package football is a thin, typed client for the football-data.org v4 API.
// It handles auth (X-Auth-Token header), request building, and per-endpoint
// decoding. It does not cache — that is the service's job.
//
// Docs: https://docs.football-data.org/general/v4/
// A free API token is required (register at https://www.football-data.org/).
package footballdata

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to the football-data.org upstream.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New builds a client. baseURL should be the v4 root, e.g.
// https://api.football-data.org/v4
//
// insecureSkipVerify disables upstream TLS verification — dev-only.
func New(baseURL, token string, timeout time.Duration, insecureSkipVerify bool) *Client {
	httpClient := &http.Client{Timeout: timeout}
	if insecureSkipVerify {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // opt-in dev-only
		}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    httpClient,
	}
}

// APIError is returned on non-2xx responses or when the body reports an error.
type APIError struct {
	StatusCode int
	Endpoint   string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("football: %s returned %d: %s", e.Endpoint, e.StatusCode, e.Message)
}

type errBody struct {
	Message   string `json:"message"`
	ErrorCode int    `json:"errorCode"`
}

// logUpstream logs one line per upstream call — request, status, and the
// response body (truncated). The token travels in a header, so the query is
// safe to log as-is.
func logUpstream(provider, path string, q url.Values, status int, dur time.Duration, err error, body []byte) {
	req := path
	if len(q) > 0 {
		req = path + "?" + q.Encode()
	}
	if err != nil {
		slog.Warn("upstream", "provider", provider, "req", req, "err", err.Error(), "dur", dur.String())
		return
	}
	slog.Info("upstream", "provider", provider, "req", req, "status", status,
		"bytes", len(body), "response", truncate(body), "dur", dur.String())
}

func truncate(b []byte) string {
	const max = 2000
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + fmt.Sprintf("…(+%d bytes)", len(b)-max)
}

// get performs a GET against path and returns the raw response body.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Auth-Token", c.token)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := doWithRetry(c.http, req)
	if err != nil {
		logUpstream("football:football-data.org", path, q, 0, time.Since(start), err, nil)
		return nil, transportError(path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	logUpstream("football:football-data.org", path, q, resp.StatusCode, time.Since(start), nil, body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e errBody
		_ = json.Unmarshal(body, &e)
		msg := e.Message
		if msg == "" {
			msg = summarizeBody(body)
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: path, Message: msg}
	}
	if looksLikeHTML(body) {
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: path, Message: proxyHint}
	}
	return body, nil
}

const proxyHint = "received an HTML page instead of JSON — a proxy or network gateway is likely blocking traffic to the football provider"

func looksLikeHTML(b []byte) bool {
	t := bytes.TrimSpace(b)
	return len(t) > 0 && t[0] == '<'
}

func summarizeBody(b []byte) string {
	if looksLikeHTML(b) {
		return proxyHint
	}
	t := bytes.TrimSpace(b)
	if len(t) > 300 {
		return string(t[:300]) + "…"
	}
	return string(t)
}

// doWithRetry retries a transient transport error once (not timeouts).
func doWithRetry(cl *http.Client, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		resp, err = cl.Do(req.Clone(req.Context()))
		if err == nil {
			return resp, nil
		}
		var nerr net.Error
		if errors.As(err, &nerr) && nerr.Timeout() {
			break
		}
		if attempt == 0 {
			time.Sleep(400 * time.Millisecond)
		}
	}
	return nil, err
}

// transportError maps a network failure to a clean APIError (no URL/token leak).
func transportError(path string, err error) *APIError {
	msg := "could not reach the football data provider"
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		msg = "football provider request timed out — please try again"
	}
	return &APIError{Endpoint: path, Message: msg}
}

// nextDay returns the day after a YYYY-MM-DD date; football-data.org treats
// dateTo as exclusive, so callers pass nextDay(to) to include the whole to-day.
func nextDay(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return t.AddDate(0, 0, 1).Format("2006-01-02")
}

// Livescores returns matches currently in play (status=LIVE covers IN_PLAY+PAUSED).
func (c *Client) Livescores(ctx context.Context) ([]Match, error) {
	q := url.Values{}
	q.Set("status", "LIVE")
	body, err := c.get(ctx, "/matches", q)
	if err != nil {
		return nil, err
	}
	var env matchesEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode livescores: %w", err)
	}
	return env.Matches, nil
}

// MatchesBetween returns matches with dates in [from, to] (inclusive of `to`).
func (c *Client) MatchesBetween(ctx context.Context, from, to string) ([]Match, error) {
	q := url.Values{}
	q.Set("dateFrom", from)
	q.Set("dateTo", nextDay(to)) // dateTo is exclusive upstream
	body, err := c.get(ctx, "/matches", q)
	if err != nil {
		return nil, err
	}
	var env matchesEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode matches: %w", err)
	}
	return env.Matches, nil
}

// MatchesByCompetition returns a competition's matches in [from, to] (inclusive
// of `to`). Use for tournament-scoped queries, e.g. code "WC" for the World Cup.
func (c *Client) MatchesByCompetition(ctx context.Context, code, from, to string) ([]Match, error) {
	q := url.Values{}
	if from != "" {
		q.Set("dateFrom", from)
	}
	if to != "" {
		q.Set("dateTo", nextDay(to)) // dateTo is exclusive upstream
	}
	body, err := c.get(ctx, "/competitions/"+code+"/matches", q)
	if err != nil {
		return nil, err
	}
	var env matchesEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode competition matches: %w", err)
	}
	return env.Matches, nil
}

// Match returns a single match by id.
func (c *Client) Match(ctx context.Context, id int64) (*Match, error) {
	body, err := c.get(ctx, "/matches/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return nil, err
	}
	var m Match
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("decode match: %w", err)
	}
	return &m, nil
}

// Standings returns the standings for a competition code (e.g. "PL").
func (c *Client) Standings(ctx context.Context, competitionCode string) (*StandingsResponse, error) {
	body, err := c.get(ctx, "/competitions/"+competitionCode+"/standings", nil)
	if err != nil {
		return nil, err
	}
	var s StandingsResponse
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("decode standings: %w", err)
	}
	return &s, nil
}

// Competitions returns the competitions the token can access.
func (c *Client) Competitions(ctx context.Context) ([]Competition, error) {
	body, err := c.get(ctx, "/competitions", nil)
	if err != nil {
		return nil, err
	}
	var env competitionsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode competitions: %w", err)
	}
	return env.Competitions, nil
}
