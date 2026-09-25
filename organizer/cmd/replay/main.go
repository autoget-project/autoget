// Command replay runs a saved /v1/plan request through the 4-stage pipeline
// locally, printing detailed step-by-step diagnostic information at each stage.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/ai/gemini"
	"github.com/autoget-project/autoget/organizer/internal/ai/grok"
	"github.com/autoget-project/autoget/organizer/internal/ai/mock"
	"github.com/autoget-project/autoget/organizer/internal/config"
	"github.com/autoget-project/autoget/organizer/internal/metadata"
	"github.com/autoget-project/autoget/organizer/internal/model"
	"github.com/autoget-project/autoget/organizer/internal/pipeline"
	stage2enricher "github.com/autoget-project/autoget/organizer/internal/pipeline/stage2_enricher"
	stage3planner "github.com/autoget-project/autoget/organizer/internal/pipeline/stage3_planner"
)

func main() {
	filePath := flag.String("file", "", "Path to JSON file containing APIPlanRequest (or '-' / omit for stdin)")
	flag.Parse()

	inputBytes, err := readInput(*filePath, flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input: %v\n", err)
		os.Exit(1)
	}

	var req model.APIPlanRequest
	if err := json.Unmarshal(inputBytes, &req); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse APIPlanRequest JSON: %v\n", err)
		os.Exit(1)
	}

	cfg := config.LoadConfig()
	var provider ai.Provider
	if cfg.Model == "" {
		cfg.Model = "gemini:gemini-2.5-flash"
	}
	if cfg.XaiAPIKey == "" && cfg.GeminiAPIKey == "" {
		fmt.Fprintf(os.Stderr, "Notice: Neither GEMINI_API_KEY nor XAI_API_KEY is configured; using mock provider (rules-only/offline mode).\n")
		provider = mock.NewProvider()
	} else {
		if cfg.Provider == "" {
			prov, err := config.ResolveProvider(cfg.Model, cfg.XaiAPIKey, cfg.GeminiAPIKey)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Provider resolution failed: %v\n", err)
				os.Exit(1)
			}
			cfg.Provider = prov
		}

		var err error
		provider, err = resolveAIProvider(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "AI Provider initialization failed: %v\n", err)
			os.Exit(1)
		}
	}

	var tmdbClient stage2enricher.TMDBSource
	if cfg.TMDBAPIKey != "" {
		tmdbClient = metadata.NewTMDB(cfg.TMDBAPIKey, cfg.TMDBLanguage)
	}
	var metatubeClient stage2enricher.JAVSource
	if cfg.MetaTubeAPIURL != "" {
		metatubeClient = metadata.NewMetatube(cfg.MetaTubeAPIURL, cfg.MetaTubeAPIKey)
	}

	actorStore := stage2enricher.NewActorStore(cfg.JavActorFile, cfg.FlareSolverrURL, provider)
	enricher := stage2enricher.NewEnricher(tmdbClient, metatubeClient, actorStore, provider)

	var tpdb stage3planner.PornSource
	if cfg.TPDBAPIToken != "" {
		tpdb = metadata.NewThePornDB(cfg.TPDBAPIToken)
	}

	pipe := pipeline.NewPipeline(provider, enricher, cfg.DownloadCompletedDir, cfg.TargetDir, tpdb)

	trace := &pipeline.StageTrace{}
	ctx := pipeline.WithTraceCollector(context.Background(), trace)

	printSection("REPLAY REQUEST INPUT")
	fmt.Printf("Dir:      %s\n", req.Dir)
	fmt.Printf("Files (%d):\n", len(req.Files))
	for _, f := range req.Files {
		fmt.Printf("  - %s\n", f)
	}
	if len(req.Metadata) > 0 {
		metaJSON, _ := json.MarshalIndent(req.Metadata, "  ", "  ")
		fmt.Printf("Metadata:\n  %s\n", string(metaJSON))
	} else {
		fmt.Println("Metadata: (empty)")
	}

	resp, err := pipe.CreatePlan(ctx, req.Dir, req.Files, req.Metadata)

	printTraceReport(trace, resp, err)

	if err != nil {
		os.Exit(1)
	}
}

func readInput(fileFlag string, args []string) ([]byte, error) {
	if fileFlag != "" && fileFlag != "-" {
		return os.ReadFile(fileFlag)
	}
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		first := strings.TrimSpace(args[0])
		if strings.HasPrefix(first, "{") {
			return []byte(first), nil
		}
		return os.ReadFile(first)
	}
	// Read from stdin
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		return io.ReadAll(os.Stdin)
	}
	return nil, errors.New("no request JSON input provided. Usage: replay -file req.json OR replay '{\"dir\":\"...\"}' OR cat req.json | replay")
}

func resolveAIProvider(cfg *config.Config) (ai.Provider, error) {
	switch cfg.Provider {
	case "grok":
		return grok.NewProvider(cfg.XaiAPIKey, ai.WithModel(cfg.Model)), nil
	case "gemini":
		return gemini.NewProvider(cfg.GeminiAPIKey, ai.WithModel(cfg.Model))
	default:
		return nil, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
}

func printSection(title string) {
	fmt.Printf("\n==================== [ %s ] ====================\n", title)
}

func printTraceReport(trace *pipeline.StageTrace, resp model.PlanResponse, planErr error) {
	// Stage 1 Report
	printSection("STAGE 1: MEDIA CLASSIFICATION")
	if trace.Stage1.RuleMatched {
		fmt.Println("Rule Match: YES (Fast path rule hit)")
	} else {
		fmt.Println("Rule Match: NO (Delegated to LLM)")
		if trace.Stage1.SearchContext.HasInfo() {
			sc := trace.Stage1.SearchContext
			fmt.Printf("Search Grounding:\n")
			fmt.Printf("  - Official Title: %s\n", sc.OfficialTitle)
			fmt.Printf("  - Detected Type:  %s\n", sc.DetectedType)
			fmt.Printf("  - Studio:         %s\n", sc.Studio)
			fmt.Printf("  - Release Date:   %s\n", sc.ReleaseDate)
			fmt.Printf("  - Actors:         %v\n", sc.Actors)
			fmt.Printf("  - Is VR:          %t\n", sc.IsVR)
			if sc.SearchSummary != "" {
				fmt.Printf("  - Summary:        %s\n", sc.SearchSummary)
			}
		} else {
			fmt.Println("Search Grounding: (none or unsupported)")
		}

		if len(trace.Stage1.Specialists) > 0 {
			fmt.Println("Specialist Checkers:")
			for _, s := range trace.Stage1.Specialists {
				if s.Err != nil {
					fmt.Printf("  [%-12s] ERROR: %v\n", s.Category, s.Err)
				} else {
					fmt.Printf("  [%-12s] Confidence: %-5s | Reason: %s\n", s.Category, s.Response.Confidence, s.Response.Reason)
				}
			}
		}

		if trace.Stage1.ArbiterUsed {
			fmt.Printf("Arbiter Decision: Invoked (Reason: %s)\n", trace.Stage1.ArbiterReason)
		} else {
			fmt.Println("Arbiter Decision: Not needed (single clear candidate)")
		}
	}
	fmt.Printf("Final Category:  %s\n", trace.Stage1.Category)
	if len(trace.Stage1.Entities) > 0 {
		entJSON, _ := json.MarshalIndent(trace.Stage1.Entities, "  ", "  ")
		fmt.Printf("Extracted Entities:\n  %s\n", string(entJSON))
	}

	// Stage 2 Report
	printSection("STAGE 2: METADATA ENRICHMENT")
	if trace.Stage2.Skipped {
		fmt.Println("Status: SKIPPED (Simple or unknown category, or enricher unconfigured)")
	} else {
		if trace.Stage2.Err != "" {
			fmt.Printf("Warning: Enrichment degraded: %s\n", trace.Stage2.Err)
		} else {
			fmt.Println("Status: SUCCESS")
		}
		en := trace.Stage2.Enriched
		fmt.Printf("Enriched Title:    %s\n", en.Title)
		if en.OriginalTitle != "" {
			fmt.Printf("Original Title:    %s\n", en.OriginalTitle)
		}
		fmt.Printf("Year:              %d\n", en.Year)
		fmt.Printf("Language:          %s\n", en.Language)
		if en.Bango != "" {
			fmt.Printf("Bango:             %s\n", en.Bango)
		}
		if len(en.Actors) > 0 {
			fmt.Printf("Actors:            %v\n", en.Actors)
		}
		fmt.Printf("Is VR:             %t\n", en.IsVR)
		fmt.Printf("Is Anim:           %t\n", en.IsAnim)
		if en.FromMadou {
			fmt.Printf("From Madou:        %t\n", en.FromMadou)
		}
	}

	// Stage 3 Report
	printSection("STAGE 3: DOMAIN PLANNING")
	fmt.Printf("Planner Selected: %s\n", trace.Stage3.PlannerName)
	if trace.Stage3.Err != "" {
		fmt.Printf("Planner Error:    %s\n", trace.Stage3.Err)
	} else {
		fmt.Printf("Raw Actions (%d):\n", len(trace.Stage3.RawPlan))
		for _, a := range trace.Stage3.RawPlan {
			if a.Action == "move" && a.Target != nil {
				fmt.Printf("  MOVE: %s -> %s\n", a.File, *a.Target)
			} else {
				fmt.Printf("  SKIP: %s\n", a.File)
			}
		}
	}

	// Stage 4 Report
	printSection("STAGE 4: POST-PROCESS & SUBTITLE PAIRING")
	if len(trace.Stage4.SubtitlesPlanned) > 0 {
		fmt.Printf("Subtitles Paired (%d):\n", len(trace.Stage4.SubtitlesPlanned))
		for _, s := range trace.Stage4.SubtitlesPlanned {
			if s.Action == "move" && s.Target != nil {
				fmt.Printf("  SUBTITLE MOVE: %s -> %s\n", s.File, *s.Target)
			} else {
				fmt.Printf("  SUBTITLE SKIP: %s\n", s.File)
			}
		}
	} else {
		fmt.Println("Subtitles Paired: (none)")
	}

	// Final Result
	printSection("FINAL EXECUTION PLAN")
	if planErr != nil {
		fmt.Printf("RESULT: FAILED (%v)\n", planErr)
	} else {
		fmt.Printf("RESULT: SUCCESS (Took %dms)\n", trace.DurationMs)
		fmt.Printf("Plan Actions (%d):\n", len(resp.Plan))
		for i, a := range resp.Plan {
			if a.Action == "move" && a.Target != nil {
				fmt.Printf("  [%2d] MOVE: %s\n       -->  %s\n", i+1, a.File, *a.Target)
			} else {
				fmt.Printf("  [%2d] SKIP: %s\n", i+1, a.File)
			}
		}
	}
	fmt.Println()
}
