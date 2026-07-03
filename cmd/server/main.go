// Command server is the entrypoint for the sports-sdv API.
//
// It loads config from the environment, wires the API-Football client, cache,
// service, and HTTP router, and serves with graceful shutdown.
package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bgmaster/sports-sdv/internal/cache"
	"github.com/bgmaster/sports-sdv/internal/config"
	"github.com/bgmaster/sports-sdv/internal/football"
	"github.com/bgmaster/sports-sdv/internal/footballdata"
	"github.com/bgmaster/sports-sdv/internal/httpapi"
	"github.com/bgmaster/sports-sdv/internal/sportmonks"
	"github.com/bgmaster/sports-sdv/internal/sports"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	// Logs go to stdout and, if configured, are also appended to a file.
	var logWriter io.Writer = os.Stdout
	if cfg.LogFile != "" {
		if mkErr := os.MkdirAll(filepath.Dir(cfg.LogFile), 0o755); mkErr == nil {
			if f, ferr := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); ferr == nil {
				defer f.Close()
				logWriter = io.MultiWriter(os.Stdout, f)
			} else {
				slog.Warn("could not open log file; stdout only", "path", cfg.LogFile, "err", ferr)
			}
		}
	}
	log := slog.New(slog.NewJSONHandler(logWriter, nil))
	slog.SetDefault(log) // so provider clients' slog calls share this JSON handler

	// Propagate the log-body cap to the clients + middleware (0 = unlimited).
	sportmonks.LogBodyMax = cfg.LogBodyMax
	football.LogBodyMax = cfg.LogBodyMax
	footballdata.LogBodyMax = cfg.LogBodyMax
	httpapi.LogBodyMax = cfg.LogBodyMax
	if cfg.LogFile != "" {
		log.Info("logging", "file", cfg.LogFile)
	}

	client := sportmonks.New(cfg.SportmonksBaseURL, cfg.SportmonksToken, cfg.UpstreamTimeout, cfg.SportmonksInsecureSkipVerify)
	if cfg.SportmonksInsecureSkipVerify || cfg.FootballInsecureSkipVerify {
		log.Warn("INSECURE_SKIP_VERIFY is enabled — upstream TLS verification is OFF (dev only)")
	}
	if cfg.FootballToken == "" {
		log.Warn("FOOTBALL_API_TOKEN is not set — football endpoints will fail until a token is provided")
	}

	mem := cache.NewMemory(time.Minute)
	defer mem.Close()

	svc := sports.New(client, mem, cfg.CacheTTL, cfg.CacheTTLLive)

	// Football provider is selectable (FOOTBALL_PROVIDER); default = API-Football.
	var fbSvc sports.FootballAPI
	switch cfg.FootballProvider {
	case "footballdata":
		fdClient := footballdata.New(cfg.FootballBaseURL, cfg.FootballToken, cfg.UpstreamTimeout, cfg.FootballInsecureSkipVerify)
		fbSvc = sports.NewFootballData(fdClient, mem, cfg.CacheTTL, cfg.CacheTTLLive)
		log.Info("football provider", "provider", "football-data.org", "baseURL", cfg.FootballBaseURL)
	default:
		fbClient := football.New(cfg.FootballBaseURL, cfg.FootballToken, cfg.UpstreamTimeout, cfg.FootballInsecureSkipVerify)
		fbSvc = sports.NewFootball(fbClient, mem, cfg.CacheTTL, cfg.CacheTTLLive)
		log.Info("football provider", "provider", "API-Football", "baseURL", cfg.FootballBaseURL)
	}
	router := httpapi.NewRouter(svc, fbSvc, log)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Serve in the background so we can wait for signals.
	go func() {
		log.Info("listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("shutdown", "err", err)
	}
}
