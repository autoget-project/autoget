package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/autoget-project/autoget/backend/downloaders"
	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/indexers/mteam"
	"github.com/autoget-project/autoget/backend/indexers/nyaa"
	"github.com/autoget-project/autoget/backend/indexers/sukebei"
	"github.com/autoget-project/autoget/backend/internal/config"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/internal/handlers"
	"github.com/autoget-project/autoget/backend/internal/notify/telegram"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
)

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

	cronjob := cron.New()
	cronjob.Start()

	downloaderMap := map[string]downloaders.IDownloader{}
	for name, dlCfg := range cfg.Downloaders {
		downloader, err := downloaders.New(name, dlCfg, db)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to create downloader")
		}
		downloaderMap[name] = downloader
		downloader.RegisterCronjobs(cronjob)
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

	service := handlers.NewService(cfg, db, indexerMap, downloaderMap)

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	rg := r.Group("/api/v1")
	service.SetupRouter(rg)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("listen")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("Server forced to shutdown")
	}

	log.Info().Msg("Server exiting")
}
