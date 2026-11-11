package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"

	"github.com/autoget-project/autoget/backend/downloaders"
	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/indexers/mteam"
	"github.com/autoget-project/autoget/backend/indexers/nyaa"
	"github.com/autoget-project/autoget/backend/indexers/sukebei"
	"github.com/autoget-project/autoget/backend/internal/auth"
	"github.com/autoget-project/autoget/backend/internal/config"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/internal/handlers"
	"github.com/autoget-project/autoget/backend/internal/notify/telegram"
	"github.com/autoget-project/autoget/backend/organizer"
)

// setupRouter wires the routes onto the engine. Only the API is guarded by
// auth; the SPA shell and its assets must stay public because document
// requests never carry the Bearer token held in localStorage.
func setupRouter(r *gin.Engine, authSvc *auth.Service, setupAPI func(*gin.RouterGroup)) {
	r.Use(handlers.SecurityHeaders())
	authSvc.SetupRouter(r)
	handlers.ServeStatic(r)
	setupAPI(r.Group("/api/v1", authSvc.Middleware()))
}

func main() {
	configPath := flag.String("c", os.Getenv("CONFIG_PATH"), "path to the configuration file")
	flag.Parse()

	if *configPath == "" {
		log.Fatal().Msg("config path is required")
	}

	cfg, err := config.ReadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to read config")
	}

	tg, err := telegram.New(cfg.Telegram)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create telegram notifier")
	}

	db, err := db.Pg(cfg.PgDSN)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}

	oc, err := organizer.NewClient(cfg.OrganizerService, &http.Client{Timeout: organizerClientTimeout})
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create organizer client")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cronjob := newCron()
	cronjob.Start()

	downloaderMap := map[string]downloaders.IDownloader{}
	for name, dlCfg := range cfg.Downloaders {
		downloader, err := downloaders.New(name, dlCfg, db, oc)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create downloader")
		}
		downloaderMap[name] = downloader
		downloader.RegisterCronjobs(ctx, cronjob)
	}

	indexerMap := map[string]indexers.IIndexer{}
	if cfg.MTeam != nil {
		normal := mteam.NewMTeam(cfg.MTeam, mteam.MTeamTypeNormal, downloaderMap[cfg.MTeam.Downloader].TorrentsDir(), db, tg)
		normal.RegisterRSSCronjob(cronjob)
		indexerMap[normal.Name()] = normal

		adult := mteam.NewMTeam(cfg.MTeam, mteam.MTeamTypeAdult, downloaderMap[cfg.MTeam.Downloader].TorrentsDir(), db, tg)
		indexerMap[adult.Name()] = adult
	}
	if cfg.Nyaa != nil {
		i := nyaa.NewClient(cfg.Nyaa, downloaderMap[cfg.Nyaa.Downloader].TorrentsDir(), db, tg)
		i.RegisterRSSCronjob(cronjob)
		indexerMap[i.Name()] = i
	}
	if cfg.Sukebei != nil {
		i := sukebei.NewClient(cfg.Sukebei, downloaderMap[cfg.Sukebei.Downloader].TorrentsDir(), db, tg)
		i.RegisterRSSCronjob(cronjob)
		indexerMap[i.Name()] = i
	}

	service := handlers.NewService(cfg, db, indexerMap, downloaderMap, oc)

	authSvc, err := auth.New(ctx, cfg.Auth)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create auth service")
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	setupRouter(r, authSvc, service.SetupRouter)

	srv := newServer(":"+cfg.Port, r)

	serveErr := make(chan error, 1)
	go func() {
		log.Info().Str("addr", cfg.Port).Msg("server started")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info().Msg("Shutting down server...")
	case err := <-serveErr:
		log.Error().Err(err).Msg("listen")
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server forced to shutdown")
	}

	// Wait for running cron jobs, then for downloader background work.
	select {
	case <-cronjob.Stop().Done():
	case <-shutdownCtx.Done():
		log.Warn().Msg("timed out waiting for cron jobs")
	}
	for name, d := range downloaderMap {
		if err := d.Shutdown(shutdownCtx); err != nil {
			log.Warn().Err(err).Str("downloader", name).Msg("downloader shutdown incomplete")
		}
	}

	log.Info().Msg("Server exiting")
}

const (
	shutdownTimeout = 10 * time.Second

	// organizerClientTimeout is generous because /v1/plan runs a synchronous
	// multi-stage AI + metadata pipeline (each AI call alone can take 60s).
	organizerClientTimeout = 5 * time.Minute

	serverReadHeaderTimeout = 10 * time.Second
	serverReadTimeout       = 30 * time.Second
	serverIdleTimeout       = 120 * time.Second
)

// newServer builds the HTTP server with read/idle timeouts. WriteTimeout is
// intentionally unset because the image proxy streams responses.
func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		IdleTimeout:       serverIdleTimeout,
	}
}

// newCron builds the scheduler with panic recovery so a panicking job is
// logged instead of crashing the process.
func newCron() *cron.Cron {
	return cron.New(cron.WithChain(cron.Recover(cron.DefaultLogger)))
}
