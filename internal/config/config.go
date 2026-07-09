// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime configuration for the service.
type Config struct {
	Port string

	// LogFile is where JSON logs are written (in addition to stdout). Empty = stdout only.
	LogFile string
	// LogBodyMax caps how many chars of request/response bodies are logged; 0 = unlimited.
	LogBodyMax int

	SportmonksToken   string
	SportmonksBaseURL string

	// SportmonksInsecureSkipVerify disables upstream TLS verification.
	// Dev-only escape hatch for broken CA stores / intercepting proxies.
	SportmonksInsecureSkipVerify bool

	// --- Football ---
	FootballProvider           string // "apifootball" (default) | "footballdata"
	FootballToken              string
	FootballBaseURL            string
	FootballInsecureSkipVerify bool

	// --- Flights (AviationStack) ---
	AviationStackKey     string
	AviationStackBaseURL string

	CacheTTL     time.Duration
	CacheTTLLive time.Duration

	UpstreamTimeout time.Duration
}

// Load reads configuration from the environment, applying sane defaults.
// It returns an error only when a required value is missing.
func Load() (*Config, error) {
	cfg := &Config{
		Port:       getenv("PORT", "8090"),
		LogFile:    getenv("LOG_FILE", "logs/server.log"),
		LogBodyMax: getint("LOG_BODY_MAX", 2000),
		// Accept either SPORTMONKS_API_TOKEN or the API_CRICKET_KEY name.
		SportmonksToken:              firstNonEmpty(os.Getenv("SPORTMONKS_API_TOKEN"), os.Getenv("API_CRICKET_KEY")),
		SportmonksBaseURL:            getenv("SPORTMONKS_BASE_URL", "https://cricket.sportmonks.com/api/v2.0"),
		SportmonksInsecureSkipVerify: getbool("SPORTMONKS_INSECURE_SKIP_VERIFY", false),
		FootballProvider:             getenv("FOOTBALL_PROVIDER", "apifootball"),
		FootballToken:                firstNonEmpty(os.Getenv("FOOTBALL_API_TOKEN"), os.Getenv("SPORTMONKS_FOOTBALL_TOKEN")),
		FootballBaseURL:              os.Getenv("FOOTBALL_BASE_URL"), // provider-default applied below
		FootballInsecureSkipVerify:   getbool("FOOTBALL_INSECURE_SKIP_VERIFY", false),
		AviationStackKey:             os.Getenv("AVIATIONSTACK_ACCESS_KEY"),
		AviationStackBaseURL:         getenv("AVIATIONSTACK_BASE_URL", "https://api.aviationstack.com/v1"),
		CacheTTL:                     getdur("CACHE_TTL", 5*time.Minute),
		CacheTTLLive:                 getdur("CACHE_TTL_LIVE", 20*time.Second),
		UpstreamTimeout:              getdur("UPSTREAM_TIMEOUT", 30*time.Second),
	}

	// Default the football base URL to match the selected provider.
	if cfg.FootballBaseURL == "" {
		if cfg.FootballProvider == "footballdata" {
			cfg.FootballBaseURL = "https://api.football-data.org/v4"
		} else {
			cfg.FootballBaseURL = "https://v3.football.api-sports.io"
		}
	}

	if cfg.SportmonksToken == "" {
		return nil, fmt.Errorf("SPORTMONKS_API_TOKEN (or API_CRICKET_KEY) is required")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getint(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
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
