// Package football is a thin, typed client for the API-Football (api-sports.io)
// v3 API. It handles auth (x-apisports-key header), request building, and the
// standard {get, parameters, errors, results, response} envelope. It does not
// cache — that is the service's job.
//
// Docs: https://www.api-football.com/documentation-v3
package football

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
	"regexp"
	"strconv"
	"strings"
	"time"
)

// logUpstream logs one line per upstream call — request, status, and the
// response body (truncated). The API key travels in a header, so the query is
// safe to log as-is.
func logUpstream(provider, path string, q url.Values, status int, dur time.Duration, err error, body []byte) {
	req := path
	if len(q) > 0 {
		req = path + "?" + q.Encode()
	}
	if err != nil {
		slog.Warn("upstream", "provider", provider, "req", req, "err", redactSecrets(err.Error()), "dur", dur.String())
		return
	}
	slog.Info("upstream", "provider", provider, "req", req, "status", status,
		"bytes", len(body), "response", truncate(body), "dur", dur.String())
}

var secretParamRe = regexp.MustCompile(`(?i)((?:api[_-]?token|api[_-]?key|apikey|token|key)=)[^&"'\s]+`)

func redactSecrets(s string) string { return secretParamRe.ReplaceAllString(s, "${1}***") }

// LogBodyMax caps how many chars of a response body are logged (0 = unlimited).
var LogBodyMax = 2000

func truncate(b []byte) string {
	if LogBodyMax <= 0 || len(b) <= LogBodyMax {
		return string(b)
	}
	return string(b[:LogBodyMax]) + fmt.Sprintf("…(+%d bytes)", len(b)-LogBodyMax)
}

// Client talks to the API-Football upstream.
type Client struct {
	baseURL string
	key     string
	http    *http.Client
}

// New builds a client. baseURL should be the v3 root, e.g.
// https://v3.football.api-sports.io
//
// insecureSkipVerify disables upstream TLS verification — dev-only.
func New(baseURL, key string, timeout time.Duration, insecureSkipVerify bool) *Client {
	httpClient := &http.Client{Timeout: timeout}
	if insecureSkipVerify {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // opt-in dev-only
		}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		http:    httpClient,
	}
}

// APIError is returned on non-2xx responses or when the envelope reports errors.
type APIError struct {
	StatusCode int
	Endpoint   string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("football: %s returned %d: %s", e.Endpoint, e.StatusCode, e.Message)
}

type envelope struct {
	Results  int             `json:"results"`
	Response json.RawMessage `json:"response"`
	Errors   json.RawMessage `json:"errors"` // [] when none; object/array of strings when present
}

// get performs a GET against path and returns the raw `response` payload.
func (c *Client) get(ctx context.Context, path string, q url.Values) (json.RawMessage, error) {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-apisports-key", c.key)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := doWithRetry(c.http, req)
	if err != nil {
		logUpstream("football:api-football", path, q, 0, time.Since(start), err, nil)
		return nil, transportError(path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	logUpstream("football:api-football", path, q, resp.StatusCode, time.Since(start), nil, body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: path, Message: statusMessage(resp.StatusCode, body)}
	}
	if looksLikeHTML(body) {
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: path, Message: proxyHint}
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode envelope for %s: %w", path, err)
	}
	// API-Football returns 200 even for quota/param/auth errors; surface them.
	if msg := errorsMessage(env.Errors); msg != "" {
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: path, Message: msg}
	}
	return env.Response, nil
}

// statusMessage turns a non-2xx HTTP status into a clear message. API-Football
// normally reports problems with HTTP 200 + an `errors` object, so a real 4xx/5xx
// usually means an auth/plan/quota issue or an intercepting proxy/gateway.
func statusMessage(code int, body []byte) string {
	switch code {
	case 401, 403:
		return "API-Football rejected the key (auth or plan restriction) — check your api-sports.io subscription"
	case 404:
		return "API-Football returned HTTP 404 — the request isn't available on your plan, or a network proxy/gateway blocked api-sports.io"
	case 429:
		return "API-Football rate limit reached (free plan is ~100 requests/day) — try again later"
	}
	if code >= 500 {
		return fmt.Sprintf("API-Football server error (HTTP %d)", code)
	}
	return summarizeBody(body)
}

// errorsMessage extracts a message from the envelope `errors` field, which may
// be an empty array (none), an object of field->message, or an array of strings.
func errorsMessage(raw json.RawMessage) string {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || string(t) == "[]" || string(t) == "null" || string(t) == "{}" {
		return ""
	}
	// object: {"token":"...", ...} or array: ["...", ...]
	var obj map[string]string
	if json.Unmarshal(t, &obj) == nil && len(obj) > 0 {
		parts := make([]string, 0, len(obj))
		for k, v := range obj {
			parts = append(parts, k+": "+v)
		}
		return strings.Join(parts, "; ")
	}
	var arr []string
	if json.Unmarshal(t, &arr) == nil && len(arr) > 0 {
		return strings.Join(arr, "; ")
	}
	return string(t)
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

func transportError(path string, err error) *APIError {
	msg := "could not reach the football data provider"
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		msg = "football provider request timed out — please try again"
	}
	return &APIError{Endpoint: path, Message: msg}
}

func decodeFixtures(raw json.RawMessage) ([]Fixture, error) {
	var out []Fixture
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode fixtures: %w", err)
	}
	return out, nil
}

// Livescores returns all matches currently in play (any league).
func (c *Client) Livescores(ctx context.Context) ([]Fixture, error) {
	q := url.Values{}
	q.Set("live", "all")
	raw, err := c.get(ctx, "/fixtures", q)
	if err != nil {
		return nil, err
	}
	return decodeFixtures(raw)
}

// FixturesByDate returns all matches on a given day (YYYY-MM-DD).
func (c *Client) FixturesByDate(ctx context.Context, date string) ([]Fixture, error) {
	q := url.Values{}
	q.Set("date", date)
	raw, err := c.get(ctx, "/fixtures", q)
	if err != nil {
		return nil, err
	}
	return decodeFixtures(raw)
}

// FixturesByLeague returns fixtures for a league+season, optionally within
// [from, to] (YYYY-MM-DD). API-Football requires league+season for date ranges.
func (c *Client) FixturesByLeague(ctx context.Context, league, season int, from, to string) ([]Fixture, error) {
	q := url.Values{}
	q.Set("league", strconv.Itoa(league))
	q.Set("season", strconv.Itoa(season))
	if from != "" && to != "" {
		q.Set("from", from)
		q.Set("to", to)
	}
	raw, err := c.get(ctx, "/fixtures", q)
	if err != nil {
		return nil, err
	}
	return decodeFixtures(raw)
}

// Fixture returns a single fixture by id.
func (c *Client) Fixture(ctx context.Context, id int64) (*Fixture, error) {
	q := url.Values{}
	q.Set("id", strconv.FormatInt(id, 10))
	raw, err := c.get(ctx, "/fixtures", q)
	if err != nil {
		return nil, err
	}
	fx, err := decodeFixtures(raw)
	if err != nil {
		return nil, err
	}
	if len(fx) == 0 {
		return nil, &APIError{StatusCode: 404, Endpoint: "/fixtures", Message: "fixture not found"}
	}
	return &fx[0], nil
}

// Standings returns the standings groups for a league+season.
func (c *Client) Standings(ctx context.Context, league, season int) ([]StandingsResponse, error) {
	q := url.Values{}
	q.Set("league", strconv.Itoa(league))
	q.Set("season", strconv.Itoa(season))
	raw, err := c.get(ctx, "/standings", q)
	if err != nil {
		return nil, err
	}
	var out []StandingsResponse
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode standings: %w", err)
	}
	return out, nil
}

// Leagues returns leagues (defaults to current=true).
func (c *Client) Leagues(ctx context.Context, q url.Values) ([]League, error) {
	if q == nil {
		q = url.Values{}
	}
	if q.Get("current") == "" && q.Get("id") == "" {
		q.Set("current", "true")
	}
	raw, err := c.get(ctx, "/leagues", q)
	if err != nil {
		return nil, err
	}
	var out []League
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode leagues: %w", err)
	}
	return out, nil
}
