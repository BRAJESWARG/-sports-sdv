package aviationstack

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
	"strings"
	"time"
)

// Client talks to the AviationStack upstream.
type Client struct {
	baseURL   string
	accessKey string
	http      *http.Client
}

// New builds a client. baseURL should be the v1 root, e.g.
// https://api.aviationstack.com/v1
func New(baseURL, accessKey string, timeout time.Duration, insecureSkipVerify bool) *Client {
	httpClient := &http.Client{Timeout: timeout}
	if insecureSkipVerify {
		httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // opt-in dev-only
		}
	}
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		accessKey: accessKey,
		http:      httpClient,
	}
}

// APIError is returned on non-2xx responses or when the body reports an error.
type APIError struct {
	StatusCode int
	Endpoint   string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("aviationstack: %s returned %d: %s", e.Endpoint, e.StatusCode, e.Message)
}

// LogBodyMax caps how many chars of a response body are logged (0 = unlimited).
var LogBodyMax = 2000

// secretParamRe redacts access_key/token/key query params from logged strings.
var secretParamRe = regexp.MustCompile(`(?i)((?:access[_-]?key|api[_-]?token|api[_-]?key|apikey|token|key)=)[^&"'\s]+`)

func redactSecrets(s string) string { return secretParamRe.ReplaceAllString(s, "${1}***") }

func truncate(b []byte) string {
	if LogBodyMax <= 0 || len(b) <= LogBodyMax {
		return string(b)
	}
	return string(b[:LogBodyMax]) + fmt.Sprintf("…(+%d bytes)", len(b)-LogBodyMax)
}

// logUpstream logs one line per upstream call, with the access_key redacted.
func logUpstream(path string, q url.Values, status int, dur time.Duration, err error, body []byte) {
	req := redactSecrets(path + "?" + q.Encode())
	if err != nil {
		slog.Warn("upstream", "provider", "flights:aviationstack", "req", req, "err", redactSecrets(err.Error()), "dur", dur.String())
		return
	}
	slog.Info("upstream", "provider", "flights:aviationstack", "req", req, "status", status,
		"bytes", len(body), "response", truncate(body), "dur", dur.String())
}

// Flights queries /flights with the given search params. The access_key is
// added here, so callers never handle the secret (and cache keys stay clean).
func (c *Client) Flights(ctx context.Context, params url.Values) ([]Flight, error) {
	q := url.Values{}
	for k, vs := range params {
		for _, v := range vs {
			if v != "" {
				q.Add(k, v)
			}
		}
	}
	q.Set("access_key", c.accessKey)

	body, err := c.get(ctx, "/flights", q)
	if err != nil {
		return nil, err
	}
	var env flightsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode flights: %w", err)
	}
	if env.Error != nil { // upstream returns HTTP 200 with an error object for some failures
		return nil, &APIError{StatusCode: 200, Endpoint: "/flights", Message: env.Error.Message}
	}
	return env.Data, nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := c.baseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		logUpstream(path, q, 0, time.Since(start), err, nil)
		return nil, transportError(path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	logUpstream(path, q, resp.StatusCode, time.Since(start), nil, body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Endpoint: path, Message: statusMessage(resp.StatusCode, body)}
	}
	return body, nil
}

func statusMessage(code int, body []byte) string {
	var env struct {
		Error *errBody `json:"error"`
	}
	_ = json.Unmarshal(body, &env)
	if env.Error != nil && env.Error.Message != "" {
		return env.Error.Message
	}
	switch code {
	case 401, 403:
		return "invalid or missing AviationStack access key"
	case 429:
		return "AviationStack rate/quota limit reached"
	}
	return fmt.Sprintf("HTTP %d", code)
}

// transportError maps a network failure to a clean APIError (no URL/key leak).
func transportError(path string, err error) *APIError {
	msg := "could not reach the flight data provider"
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		msg = "flight provider request timed out — please try again"
	}
	return &APIError{Endpoint: path, Message: msg}
}
