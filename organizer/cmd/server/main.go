// Command server is the AutoGet Organizer REST service entry point: it runs
// startup checks, resolves the AI provider from the MODEL env, wires the
// pipeline / executor / handlers and serves the REST routes with graceful
// shutdown.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/ai/gemini"
	"github.com/autoget-project/autoget/organizer/internal/ai/grok"
	"github.com/autoget-project/autoget/organizer/internal/config"
	"github.com/autoget-project/autoget/organizer/internal/handler"
	"github.com/autoget-project/autoget/organizer/internal/metadata"
	"github.com/autoget-project/autoget/organizer/internal/pipeline"
	stage2enricher "github.com/autoget-project/autoget/organizer/internal/pipeline/stage2_enricher"
	stage3planner "github.com/autoget-project/autoget/organizer/internal/pipeline/stage3_planner"
	"github.com/autoget-project/autoget/organizer/internal/service"
	"github.com/autoget-project/autoget/organizer/upload"
)

const defaultPort = "8000"

func main() {
	cfg := config.LoadConfig()
	if err := config.StartupCheck(cfg); err != nil {
		log.Fatalf("startup check failed: %v", err)
	}

	provider, err := resolveProvider(cfg)
	if err != nil {
		log.Fatalf("provider resolution failed: %v", err)
	}

	tmdbClient := metadata.NewTMDB(cfg.TMDBAPIKey, cfg.TMDBLanguage)
	metatubeClient := metadata.NewMetatube(cfg.MetaTubeAPIURL, cfg.MetaTubeAPIKey)
	actorStore := stage2enricher.NewActorStore(cfg.JavActorFile, cfg.FlareSolverrURL, provider)
	enricher := stage2enricher.NewEnricher(tmdbClient, metatubeClient, actorStore, provider)

	// Explicit nil interface (not a typed-nil *ThePornDBClient): the porn
	// planner detects `tpdb == nil` and silently falls back to the local
	// naming chain when TPDB_API_TOKEN is not configured.
	var tpdb stage3planner.PornSource
	if cfg.TPDBAPIToken != "" {
		tpdb = metadata.NewThePornDB(cfg.TPDBAPIToken)
	}
	pipe := pipeline.NewPipeline(provider, enricher, cfg.DownloadCompletedDir, cfg.TargetDir, tpdb)
	exec := service.NewExecutor(cfg.DownloadCompletedDir, cfg.TargetDir)

	uploadStore, err := upload.NewStore(cfg.UploadTempDir, cfg.DownloadCompletedDir, cfg.UploadReserveBytes)
	if err != nil {
		log.Fatalf("failed to initialize upload store: %v", err)
	}
	uploadHandler := upload.NewHandler(uploadStore)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/plan", handler.NewPlanHandler(pipe).Handle)
	mux.HandleFunc("POST /v1/execute", handler.NewExecuteHandler(exec).Handle)
	mux.HandleFunc("POST /v1/replan-with-hint", handler.NewReplanHandler(provider).Handle)
	upload.RegisterRoutes(mux, uploadHandler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	uploadGC := upload.NewGC(uploadStore, time.Duration(cfg.UploadExpireHours)*time.Hour)
	go uploadGC.Run(ctx, 1*time.Hour)

	go func() {
		log.Printf("organizer server listening on :%s (provider=%s model=%s)", port, provider.Name(), cfg.Model)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server failed: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	log.Printf("organizer server stopped")
}

// resolveProvider builds the ai.Provider matching the MODEL prefix resolved by
// StartupCheck; unknown providers are fatal.
func resolveProvider(cfg *config.Config) (ai.Provider, error) {
	switch cfg.Provider {
	case "grok":
		return grok.NewProvider(cfg.XaiAPIKey, ai.WithModel(cfg.Model)), nil
	case "gemini":
		return gemini.NewProvider(cfg.GeminiAPIKey, ai.WithModel(cfg.Model))
	default:
		return nil, errors.New("MODEL must reference a grok or gemini provider (use xai: or gemini: prefix)")
	}
}
