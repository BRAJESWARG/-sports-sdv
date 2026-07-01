// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all runtime configuration for the service.
type Config struct {
	Port string

	SportmonksToken   string
	SportmonksBaseURL string

	// SportmonksInsecureSkipVerify disables upstream TLS verification.
	// Dev-only escape hatch for broken CA stores / intercepting proxies.
	SportmonksInsecureSkipVerify bool

	// SportmonksMock serves embedded sample data instead of calling upstream.
	// Useful for offline development or when a proxy blocks egress.
	SportmonksMock bool

	// --- SportMonks Football (v3) ---
	FootballToken              string
	FootballBaseURL            string
	FootballInsecureSkipVerify bool
	FootballMock               bool

	CacheTTL     time.Duration
	CacheTTLLive time.Duration

	UpstreamTimeout time.Duration
}

// Load reads configuration from the environment, applying sane defaults.
// It returns an error only when a required value is missing.
func Load() (*Config, error) {
	cfg := &Config{
		Port: getenv("PORT", "8090"),
		// Accept either SPORTMONKS_API_TOKEN or the API_CRICKET_KEY name.
		SportmonksToken:              firstNonEmpty(os.Getenv("SPORTMONKS_API_TOKEN"), os.Getenv("API_CRICKET_KEY")),
		SportmonksBaseURL:            getenv("SPORTMONKS_BASE_URL", "https://cricket.sportmonks.com/api/v2.0"),
		SportmonksInsecureSkipVerify: getbool("SPORTMONKS_INSECURE_SKIP_VERIFY", false),
		SportmonksMock:               getbool("SPORTMONKS_MOCK", false),
		FootballToken:                firstNonEmpty(os.Getenv("FOOTBALL_API_TOKEN"), os.Getenv("SPORTMONKS_FOOTBALL_TOKEN")),
		FootballBaseURL:              getenv("FOOTBALL_BASE_URL", "https://api.sportmonks.com/v3/football"),
		FootballInsecureSkipVerify:   getbool("FOOTBALL_INSECURE_SKIP_VERIFY", false),
		FootballMock:                 getbool("FOOTBALL_MOCK", false),
		CacheTTL:                     getdur("CACHE_TTL", 5*time.Minute),
		CacheTTLLive:                 getdur("CACHE_TTL_LIVE", 20*time.Second),
		UpstreamTimeout:              getdur("UPSTREAM_TIMEOUT", 10*time.Second),
	}

	if cfg.SportmonksToken == "" && !cfg.SportmonksMock {
		return nil, fmt.Errorf("SPORTMONKS_API_TOKEN (or API_CRICKET_KEY) is required (or set SPORTMONKS_MOCK=true for offline sample data)")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func getbool(key string, fallback bool) bool {
	switch os.Getenv(key) {
	case "1", "true", "TRUE", "True", "yes", "on":
		return true
	case "0", "false", "FALSE", "False", "no", "off":
		return false
	default:
		return fallback
	}
}

func getdur(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
