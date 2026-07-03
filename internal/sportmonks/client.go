// Package sportmonks is a thin, typed client for the SportMonks Cricket API v2.0.
// It handles auth (api_token query param), request building, the {"data","meta"}
// envelope, and the nested {"data": ...} include wrappers. It does not cache —
// that is the service layer's job.
//
// Docs: https://docs.sportmonks.com/v2/cricket-api
package sportmonks

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

// logUpstream logs one line per upstream call — the request (api_token redacted),
// status, and the response body (truncated) — so you can see what came back.
func logUpstream(provider, path string, q url.Values, status int, dur time.Duration, err error, body []byte) {
	rq := url.Values{}
	for k, v := range q {
		if k == "api_token" {
			rq.Set(k, "***")
		} else {
			rq[k] = v
		}
	}
	req := path
	if len(rq) > 0 {
		req = path + "?" + rq.Encode()
	}
	if err != nil {
		slog.Warn("upstream", "provider", provider, "req", req, "err", redactSecrets(err.Error()), "dur", dur.String())
		return
	}
	slog.Info("upstream", "provider", provider, "req", req, "status", status,
		"bytes", len(body), "response", truncate(body), "dur", dur.String())
}

// secretParamRe matches key/token query params so their values can be redacted
// from anything logged (Go's transport errors embed the full request URL).
var secretParamRe = regexp.MustCompile(`(?i)((?:api[_-]?token|api[_-]?key|apikey|token|key)=)[^&"'\s]+`)

func redactSecrets(s string) string { return secretParamRe.ReplaceAllString(s, "${1}***") }

// LogBodyMax caps how many chars of a response body are logged (0 = unlimited).
// Set from config at startup.
var LogBodyMax = 2000

// truncate renders a body for logging, capping its length.
func truncate(b []byte) string {
	if LogBodyMax <= 0 || len(b) <= LogBodyMax {
		return string(b)
	}
	return string(b[:LogBodyMax]) + fmt.Sprintf("…(+%d bytes)", len(b)-LogBodyMax)
}

// Client talks to the SportMonks Cricket upstream.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New builds a client. baseURL should be the v2.0 root, e.g.
// https://cricket.sportmonks.com/api/v2.0
//
// insecureSkipVerify disables upstream TLS certificate verification. It exists
// only to unblock local development behind a broken CA store or a
// TLS-intercepting proxy. NEVER enable it in production — it makes the
// connection vulnerable to man-in-the-middle attacks.
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

// APIError is returned on non-2xx responses or when the envelope reports an error.
type APIError struct {
	StatusCode int
	Endpoint   string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("sportmonks: %s returned %d: %s", e.Endpoint, e.StatusCode, e.Message)
}

type envelope struct {
	Data    json.RawMessage `json:"data"`
	Error   *errorBody      `json:"error"`
	Message string          `json:"message"`
}

type errorBody struct {
	Message string          `json:"message"`
	Code    json.RawMessage `json:"code"`
}

// get performs a GET against path, always injecting the api_token, and returns
// the raw `data` payload from the envelope.
func (c *Client) get(ctx context.Context, path string, q url.Values) (json.RawMessage, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("api_token", c.token)

	u := c.baseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := doWithRetry(c.http, req)
	if err != nil {
		logUpstream("cricket:sportmonks", path, q, 0, time.Since(start), err, nil)
		return nil, transportError(path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MiB cap
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	logUpstream("cricket:sportmonks", path, q, resp.StatusCode, time.Since(start), nil, body)

	var env envelope
	// Ignore unmarshal error here; we fall back to raw body in the error path.
	_ = json.Unmarshal(body, &env)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := env.Message
		if env.Error != nil && env.Error.Message != "" {
			msg = env.Error.Message
		}
		if msg == "" {
			msg = summarizeBody(body)
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: path, Message: msg}
	}
	if env.Error != nil && env.Error.Message != "" {
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: path, Message: env.Error.Message}
	}
	// A 2xx that isn't JSON almost always means something (a proxy/gateway)
	// intercepted the request before it reached SportMonks.
	if looksLikeHTML(body) {
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: path, Message: proxyHint}
	}
	return env.Data, nil
}

const proxyHint = "received an HTML page instead of JSON — a TLS-intercepting proxy or network gateway is likely blocking traffic between this server and SportMonks"

func looksLikeHTML(b []byte) bool {
	t := bytes.TrimSpace(b)
	return len(t) > 0 && t[0] == '<'
}

// summarizeBody produces a short, log-friendly message from an error body.
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

// doWithRetry performs the request, retrying once on a transient transport
// error after a short backoff. Timeouts are NOT retried (that just doubles the
// wait). GET requests are safe to retry.
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

// transportError maps a network failure to a clean APIError. It never includes
// the request URL, which carries the api_token.
func transportError(path string, err error) *APIError {
	msg := "could not reach SportMonks"
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		msg = "SportMonks request timed out — please try again"
	}
	return &APIError{Endpoint: path, Message: msg}
}

// withIncludes returns a copy of q with the include param set (if non-empty).
func withIncludes(q url.Values, includes ...string) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = v
	}
	if len(includes) > 0 {
		out.Set("include", strings.Join(includes, ","))
	}
	return out
}

// Livescores returns matches currently in play or scheduled for today, with
// enough detail (batting/bowling) to build a live scorecard.
func (c *Client) Livescores(ctx context.Context, q url.Values) ([]Fixture, error) {
	raw, err := c.get(ctx, "/livescores", withIncludes(q,
		"league", "localteam", "visitorteam", "runs", "batting.batsman", "bowling.bowler"))
	if err != nil {
		return nil, err
	}
	return decodeSlice[Fixture](raw, "livescores")
}

// Fixtures returns matches matching the query (filters like filter[starts_between]).
func (c *Client) Fixtures(ctx context.Context, q url.Values) ([]Fixture, error) {
	raw, err := c.get(ctx, "/fixtures", withIncludes(q, "league", "localteam", "visitorteam", "runs"))
	if err != nil {
		return nil, err
	}
	return decodeSlice[Fixture](raw, "fixtures")
}

// Fixture returns a single match with full scorecard includes.
func (c *Client) Fixture(ctx context.Context, id int) (*Fixture, error) {
	inc := withIncludes(nil, "localteam", "visitorteam", "runs", "batting.batsman", "bowling.bowler")
	raw, err := c.get(ctx, "/fixtures/"+strconv.Itoa(id), inc)
	if err != nil {
		return nil, err
	}
	var f Fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("decode fixture: %w", err)
	}
	return &f, nil
}

// StandingsBySeason returns the standings table for a season.
func (c *Client) StandingsBySeason(ctx context.Context, seasonID int) ([]Standing, error) {
	raw, err := c.get(ctx, "/standings/season/"+strconv.Itoa(seasonID), withIncludes(nil, "team"))
	if err != nil {
		return nil, err
	}
	return decodeSlice[Standing](raw, "standings")
}

// TeamRankings returns global ICC team rankings. Filter with
// filter[type]=TEST|ODI|T20I and filter[gender]=men|women.
func (c *Client) TeamRankings(ctx context.Context, q url.Values) ([]RankingType, error) {
	raw, err := c.get(ctx, "/team-rankings", q)
	if err != nil {
		return nil, err
	}
	return decodeSlice[RankingType](raw, "team-rankings")
}

// Leagues returns competitions.
func (c *Client) Leagues(ctx context.Context, q url.Values) ([]League, error) {
	raw, err := c.get(ctx, "/leagues", q)
	if err != nil {
		return nil, err
	}
	return decodeSlice[League](raw, "leagues")
}

// Seasons returns seasons.
func (c *Client) Seasons(ctx context.Context, q url.Values) ([]Season, error) {
	raw, err := c.get(ctx, "/seasons", q)
	if err != nil {
		return nil, err
	}
	return decodeSlice[Season](raw, "seasons")
}

func decodeSlice[T any](raw json.RawMessage, what string) ([]T, error) {
	var out []T
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode %s: %w", what, err)
	}
	return out, nil
}
