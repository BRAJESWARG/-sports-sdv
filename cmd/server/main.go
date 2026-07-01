// Command server is the entrypoint for the sports-sdv API.
//
// It loads config from the environment, wires the API-Football client, cache,
// service, and HTTP router, and serves with graceful shutdown.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bgmaster/sports-sdv/internal/cache"
	"github.com/bgmaster/sports-sdv/internal/config"
	"github.com/bgmaster/sports-sdv/internal/football"
	"github.com/bgmaster/sports-sdv/internal/httpapi"
	"github.com/bgmaster/sports-sdv/internal/sportmonks"
	"github.com/bgmaster/sports-sdv/internal/sports"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	client := sportmonks.New(cfg.SportmonksBaseURL, cfg.SportmonksToken, cfg.UpstreamTimeout, cfg.SportmonksInsecureSkipVerify)
	fbClient := football.New(cfg.FootballBaseURL, cfg.FootballToken, cfg.UpstreamTimeout, cfg.FootballInsecureSkipVerify)
	if cfg.SportmonksInsecureSkipVerify || cfg.FootballInsecureSkipVerify {
		log.Warn("INSECURE_SKIP_VERIFY is enabled — upstream TLS verification is OFF (dev only)")
	}
	if cfg.FootballToken == "" {
		log.Warn("FOOTBALL_API_TOKEN is not set — football endpoints will fail until a token is provided")
	}

	mem := cache.NewMemory(time.Minute)
	defer mem.Close()

	svc := sports.New(client, mem, cfg.CacheTTL, cfg.CacheTTLLive)
	fbSvc := sports.NewFootball(fbClient, mem, cfg.CacheTTL, cfg.CacheTTLLive)
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
