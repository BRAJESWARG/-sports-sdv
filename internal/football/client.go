// Package football is a thin, typed client for the SportMonks Football API v3.
// It handles auth (Authorization header), request building, and the v3
// {"data","pagination"} envelope. Unlike the cricket client, v3 includes are
// inline (no {"data": ...} wrappers). It does not cache — that is the service's job.
//
// Docs: https://docs.sportmonks.com/football
package football

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to the SportMonks Football v3 upstream.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	mock    bool
}

// New builds a live client. baseURL should be the v3 football root, e.g.
// https://api.sportmonks.com/v3/football
//
// insecureSkipVerify disables upstream TLS verification — dev-only, see the
// cricket client for the same caveat.
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

// NewMock builds a client that serves embedded sample data (see fixtures.go)
// instead of making any network call.
func NewMock() *Client { return &Client{mock: true} }

// APIError is returned on non-2xx responses or when the envelope reports an error.
type APIError struct {
	StatusCode int
	Endpoint   string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("football: %s returned %d: %s", e.Endpoint, e.StatusCode, e.Message)
}

type envelope struct {
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

// get performs a GET against path and returns the raw `data` payload.
func (c *Client) get(ctx context.Context, path string, q url.Values) (json.RawMessage, error) {
	if c.mock {
		return mockData(path)
	}
	if q == nil {
		q = url.Values{}
	}
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	// v3 auth: token in the Authorization header (api_token query also works).
	req.Header.Set("Authorization", c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var env envelope
	_ = json.Unmarshal(body, &env)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := env.Message
		if msg == "" {
			msg = summarizeBody(body)
		}
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: path, Message: msg}
	}
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

// inc builds a query with a v3 include list (semicolon-separated).
func inc(names ...string) url.Values {
	q := url.Values{}
	if len(names) > 0 {
		q.Set("include", strings.Join(names, ";"))
	}
	return q
}

var fixtureIncludes = []string{"participants", "scores", "state", "league"}

// Livescores returns matches currently in play.
func (c *Client) Livescores(ctx context.Context) ([]Fixture, error) {
	raw, err := c.get(ctx, "/livescores/inplay", inc(fixtureIncludes...))
	if err != nil {
		return nil, err
	}
	return decodeSlice[Fixture](raw, "livescores")
}

// Fixtures returns fixtures matching q (plus the standard includes).
func (c *Client) Fixtures(ctx context.Context, q url.Values) ([]Fixture, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("include", strings.Join(fixtureIncludes, ";"))
	raw, err := c.get(ctx, "/fixtures", q)
	if err != nil {
		return nil, err
	}
	return decodeSlice[Fixture](raw, "fixtures")
}

// FixturesBetween returns fixtures whose start date falls in [from, to] (YYYY-MM-DD).
func (c *Client) FixturesBetween(ctx context.Context, from, to string) ([]Fixture, error) {
	raw, err := c.get(ctx, "/fixtures/between/"+from+"/"+to, inc(fixtureIncludes...))
	if err != nil {
		return nil, err
	}
	return decodeSlice[Fixture](raw, "fixtures")
}

// Fixture returns a single match with includes.
func (c *Client) Fixture(ctx context.Context, id int64) (*Fixture, error) {
	q := inc("participants", "scores", "state", "league", "round")
	raw, err := c.get(ctx, "/fixtures/"+strconv.FormatInt(id, 10), q)
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
func (c *Client) StandingsBySeason(ctx context.Context, seasonID int64) ([]Standing, error) {
	q := inc("participant", "details")
	raw, err := c.get(ctx, "/standings/season/"+strconv.FormatInt(seasonID, 10), q)
	if err != nil {
		return nil, err
	}
	return decodeSlice[Standing](raw, "standings")
}

// Leagues returns competitions.
func (c *Client) Leagues(ctx context.Context, q url.Values) ([]League, error) {
	raw, err := c.get(ctx, "/leagues", q)
	if err != nil {
		return nil, err
	}
	return decodeSlice[League](raw, "leagues")
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
