package highlightly

import (
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

// Client talks to the Highlightly football + cricket upstreams. Both share the
// x-rapidapi-key header auth and the same {data:[...]} envelope, but live on
// different hosts.
type Client struct {
	footballBase string // e.g. https://sports.highlightly.net/football
	cricketBase  string // e.g. https://cricket.highlightly.net
	apiKey       string
	timezone     string // e.g. Asia/Kolkata
	http         *http.Client
}

// New builds a client.
func New(footballBase, cricketBase, apiKey, timezone string, timeout time.Duration, insecureSkipVerify bool) *Client {
	httpClient := &http.Client{Timeout: timeout}
	if insecureSkipVerify {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // opt-in dev-only
		}
	}
	if timezone == "" {
		timezone = "Asia/Kolkata"
	}
	return &Client{
		footballBase: strings.TrimRight(footballBase, "/"),
		cricketBase:  strings.TrimRight(cricketBase, "/"),
		apiKey:       apiKey,
		timezone:     timezone,
		http:         httpClient,
	}
}

// APIError is returned on non-2xx responses.
type APIError struct {
	StatusCode int
	Endpoint   string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("highlightly: %s returned %d: %s", e.Endpoint, e.StatusCode, e.Message)
}

// LogBodyMax caps how many chars of a response body are logged (0 = unlimited).
var LogBodyMax = 2000

// secretValueRe redacts the api key value if it ever appears in a logged string.
var secretValueRe = regexp.MustCompile(`(?i)(x-rapidapi-key[:=]\s*)[^\s"']+`)

func redactSecrets(s string) string { return secretValueRe.ReplaceAllString(s, "${1}***") }

func truncate(b []byte) string {
	if LogBodyMax <= 0 || len(b) <= LogBodyMax {
		return string(b)
	}
	return string(b[:LogBodyMax]) + fmt.Sprintf("…(+%d bytes)", len(b)-LogBodyMax)
}

func logUpstream(sport, url string, status int, dur time.Duration, err error, body []byte) {
	if err != nil {
		slog.Warn("upstream", "provider", "highlightly:"+sport, "req", redactSecrets(url), "err", redactSecrets(err.Error()), "dur", dur.String())
		return
	}
	slog.Info("upstream", "provider", "highlightly:"+sport, "req", url, "status", status,
		"bytes", len(body), "response", truncate(body), "dur", dur.String())
}

// hlPageLimit is Highlightly's per-request row cap; hlMaxOffset bounds how many
// pages one date can fetch (so a huge day doesn't fan out unboundedly).
const (
	hlPageLimit = 100
	hlMaxOffset = 300 // up to 3 pages / 300 matches per date
)

// FootballMatches returns all football matches for a single date (YYYY-MM-DD),
// paging through the 100-row limit so busy days aren't truncated.
func (c *Client) FootballMatches(ctx context.Context, date string) ([]FootballMatch, error) {
	var all []FootballMatch
	for offset := 0; offset < hlMaxOffset; offset += hlPageLimit {
		body, err := c.get(ctx, "football", c.footballBase+"/matches", date, offset)
		if err != nil {
			return nil, err
		}
		var env listEnvelope[FootballMatch]
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, fmt.Errorf("decode football matches: %w", err)
		}
		all = append(all, env.Data...)
		if len(env.Data) == 0 || offset+hlPageLimit >= env.Pagination.TotalCount {
			break
		}
	}
	return all, nil
}

// CricketMatches returns all cricket matches for a single date, paginated.
func (c *Client) CricketMatches(ctx context.Context, date string) ([]CricketMatch, error) {
	var all []CricketMatch
	for offset := 0; offset < hlMaxOffset; offset += hlPageLimit {
		body, err := c.get(ctx, "cricket", c.cricketBase+"/matches", date, offset)
		if err != nil {
			return nil, err
		}
		var env listEnvelope[CricketMatch]
		if err := json.Unmarshal(body, &env); err != nil {
			return nil, fmt.Errorf("decode cricket matches: %w", err)
		}
		all = append(all, env.Data...)
		if len(env.Data) == 0 || offset+hlPageLimit >= env.Pagination.TotalCount {
			break
		}
	}
	return all, nil
}

func (c *Client) get(ctx context.Context, sport, base, date string, offset int) ([]byte, error) {
	q := url.Values{}
	if date != "" {
		q.Set("date", date)
	}
	q.Set("timezone", c.timezone)
	q.Set("limit", strconv.Itoa(hlPageLimit))
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	u := base + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("x-rapidapi-key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		logUpstream(sport, u, 0, time.Since(start), err, nil)
		return nil, transportError(base, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	logUpstream(sport, u, resp.StatusCode, time.Since(start), nil, body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: base, Message: statusMessage(resp.StatusCode, body)}
	}
	return body, nil
}

func statusMessage(code int, body []byte) string {
	var env struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(body, &env)
	if env.Message != "" {
		return env.Message
	}
	if env.Error != "" {
		return env.Error
	}
	switch code {
	case 401, 403:
		return "invalid or missing Highlightly API key"
	case 429:
		return "Highlightly rate/quota limit reached"
	}
	return fmt.Sprintf("HTTP %d", code)
}

func transportError(endpoint string, err error) *APIError {
	msg := "could not reach the Highlightly provider"
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		msg = "Highlightly request timed out — please try again"
	}
	return &APIError{Endpoint: endpoint, Message: msg}
}
