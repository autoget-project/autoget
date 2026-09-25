// Command replay runs a saved /v1/plan request through the 4-stage pipeline
// locally, or inspects historical OpenTelemetry trace spans offline from .local/traces.jsonl,
// printing detailed step-by-step diagnostic information at each stage.
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
	"time"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/ai/gemini"
	"github.com/autoget-project/autoget/organizer/internal/ai/grok"
	"github.com/autoget-project/autoget/organizer/internal/ai/mock"
	"github.com/autoget-project/autoget/organizer/internal/config"
	"github.com/autoget-project/autoget/organizer/internal/metadata"
	"github.com/autoget-project/autoget/organizer/internal/model"
	"github.com/autoget-project/autoget/organizer/internal/pipeline"
	stage1classifier "github.com/autoget-project/autoget/organizer/internal/pipeline/stage1_classifier"
	stage2enricher "github.com/autoget-project/autoget/organizer/internal/pipeline/stage2_enricher"
	stage3planner "github.com/autoget-project/autoget/organizer/internal/pipeline/stage3_planner"
	"github.com/autoget-project/autoget/organizer/internal/telemetry"
)

func main() {
	filePath := flag.String("file", "", "Path to JSON file containing APIPlanRequest (or '-' / omit for stdin)")
	traceIDFlag := flag.String("trace-id", "", "Replay offline diagnostics for a specific trace ID from local trace file")
	lastFlag := flag.Bool("last", false, "Replay offline diagnostics for the most recent trace in local trace file")
	flag.Parse()

	cfg := config.LoadConfig()
	traceFilePath := cfg.Telemetry.FilePath
	if traceFilePath == "" {
		traceFilePath = ".local/traces.jsonl"
	}

	// 1. Offline replay mode (--last or --trace-id)
	if *lastFlag && *traceIDFlag != "" {
		fmt.Fprintf(os.Stderr, "Error: --last and --trace-id flags are mutually exclusive\n")
		os.Exit(1)
	}

	if *lastFlag || *traceIDFlag != "" {
		targetTraceID := *traceIDFlag
		if *lastFlag {
			latestID, err := telemetry.GetLatestTraceID(traceFilePath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error locating latest trace in %s: %v\n", traceFilePath, err)
				os.Exit(1)
			}
			targetTraceID = latestID
		}

		spans, err := telemetry.ReadTraceSpans(traceFilePath, targetTraceID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading trace %s from %s: %v\n", targetTraceID, traceFilePath, err)
			os.Exit(1)
		}

		printOfflineTraceReport(targetTraceID, spans)
		return
	}

	// 2. Live execution replay mode (-file or stdin)
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

	// Set up local file telemetry exporter so live replay also persists trace spans
	telCfg := cfg.Telemetry
	telCfg.Exporter = "file"
	telCfg.FilePath = traceFilePath
	telCfg.SampleRatio = 1.0
	_, err = telemetry.Init(telCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize telemetry file exporter: %v\n", err)
	}

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

	pipe := pipeline.NewPipeline(provider, enricher, cfg.DownloadCompletedDir, cfg.TargetDir, tpdb, telemetry.Tracer("replay"))

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

	runCtx := context.Background()
	resp, planErr := pipe.CreatePlan(runCtx, req.Dir, req.Files, req.Metadata)

	// Flush and Shutdown telemetry before rendering reports to guarantee span persistence
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = telemetry.Shutdown(shutdownCtx)
	cancel()

	// Locate the latest trace ID to print the full diagnostic report
	latestTraceID, lErr := telemetry.GetLatestTraceID(traceFilePath)
	if lErr == nil && latestTraceID != "" {
		if spans, sErr := telemetry.ReadTraceSpans(traceFilePath, latestTraceID); sErr == nil && len(spans) > 0 {
			printOfflineTraceReport(latestTraceID, spans)
			if planErr != nil {
				os.Exit(1)
			}
			return
		}
	}

	// Fallback print if reading trace failed
	printLiveFallbackReport(resp, planErr)
	if planErr != nil {
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

func printOfflineTraceReport(traceID string, spans []telemetry.SpanRecord) {
	var (
		rootSpan   *telemetry.SpanRecord
		stage1Span *telemetry.SpanRecord
		stage2Span *telemetry.SpanRecord
		stage3Span *telemetry.SpanRecord
		stage4Span *telemetry.SpanRecord
	)

	for i := range spans {
		s := &spans[i]
		switch s.Name {
		case telemetry.SpanPipelineCreatePlan:
			rootSpan = s
		case telemetry.SpanStage1Classify:
			stage1Span = s
		case telemetry.SpanStage2Enrich:
			stage2Span = s
		case telemetry.SpanStage3Plan:
			stage3Span = s
		case telemetry.SpanStage4PostProcess:
			stage4Span = s
		}
	}

	printSection(fmt.Sprintf("TRACE DIAGNOSTICS: %s", traceID))

	// Replay request inputs from root span
	if rootSpan != nil {
		fmt.Printf("Dir:      %s\n", rootSpan.StringAttr(telemetry.AttrOrganizerDir))
		if filesCount := rootSpan.IntAttr(telemetry.AttrOrganizerFilesCount); filesCount > 0 {
			fmt.Printf("Files (%d):\n", filesCount)
			if fAttr, ok := rootSpan.Attributes[telemetry.AttrOrganizerFiles]; ok {
				if fList, ok := fAttr.([]interface{}); ok {
					for _, f := range fList {
						fmt.Printf("  - %v\n", f)
					}
				}
			}
		}
		if metaStr := rootSpan.StringAttr(telemetry.AttrOrganizerMetadataJSON); metaStr != "" {
			var m map[string]interface{}
			if err := json.Unmarshal([]byte(metaStr), &m); err == nil {
				metaJSON, _ := json.MarshalIndent(m, "  ", "  ")
				fmt.Printf("Metadata:\n  %s\n", string(metaJSON))
			} else {
				fmt.Printf("Metadata: %s\n", metaStr)
			}
		}
	}

	// Stage 1 Report
	printSection("STAGE 1: MEDIA CLASSIFICATION")
	if stage1Span != nil {
		ruleMatched := stage1Span.BoolAttr(telemetry.AttrStage1RuleMatched)
		if ruleMatched {
			fmt.Println("Rule Match: YES (Fast path rule hit)")
		} else {
			fmt.Println("Rule Match: NO (Delegated to LLM)")

			// Search context
			scStr := stage1Span.StringAttr(telemetry.AttrStage1SearchContextJSON)
			if scStr != "" {
				var sc stage1classifier.SearchContext
				if err := json.Unmarshal([]byte(scStr), &sc); err == nil && sc.HasInfo() {
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
			} else {
				fmt.Println("Search Grounding: (none or unsupported)")
			}

			// Specialists
			specStr := stage1Span.StringAttr(telemetry.AttrStage1SpecialistsJSON)
			if specStr != "" {
				var specs []stage1classifier.CheckerResult
				if err := json.Unmarshal([]byte(specStr), &specs); err == nil && len(specs) > 0 {
					fmt.Println("Specialist Checkers:")
					for _, s := range specs {
						if s.Err != nil {
							fmt.Printf("  [%-12s] ERROR: %v\n", s.Category, s.Err)
						} else {
							fmt.Printf("  [%-12s] Confidence: %-5s | Reason: %s\n", s.Category, s.Response.Confidence, s.Response.Reason)
						}
					}
				}
			}

			// Arbiter
			arbiterUsed := stage1Span.BoolAttr(telemetry.AttrStage1ArbiterUsed)
			if arbiterUsed {
				fmt.Printf("Arbiter Decision: Invoked (Reason: %s)\n", stage1Span.StringAttr(telemetry.AttrStage1ArbiterReason))
			} else {
				fmt.Println("Arbiter Decision: Not needed (single clear candidate)")
			}
		}
		fmt.Printf("Final Category:  %s\n", stage1Span.StringAttr(telemetry.AttrStage1Category))
	} else {
		fmt.Println("Stage 1 Span: NOT FOUND")
	}

	// Stage 2 Report
	printSection("STAGE 2: METADATA ENRICHMENT")
	if stage2Span != nil {
		skipped := stage2Span.BoolAttr(telemetry.AttrStage2Skipped)
		if skipped {
			fmt.Println("Status: SKIPPED (Simple or unknown category, or enricher unconfigured)")
		} else {
			// Check if any degrade events recorded
			var degradeWarnings []string
			for _, ev := range stage2Span.Events {
				if ev.Name == "stage2_degraded" {
					if w, ok := ev.Attributes["warning"]; ok {
						degradeWarnings = append(degradeWarnings, fmt.Sprintf("%v", w))
					}
				}
			}
			if len(degradeWarnings) > 0 {
				fmt.Printf("Warning: Enrichment degraded: %s\n", strings.Join(degradeWarnings, "; "))
			} else {
				fmt.Println("Status: SUCCESS")
			}

			if title := stage2Span.StringAttr(telemetry.AttrStage2EnrichedTitle); title != "" {
				fmt.Printf("Enriched Title:    %s\n", title)
			}
			if year := stage2Span.IntAttr(telemetry.AttrStage2EnrichedYear); year > 0 {
				fmt.Printf("Year:              %d\n", year)
			}
			if bango := stage2Span.StringAttr(telemetry.AttrStage2EnrichedBango); bango != "" {
				fmt.Printf("Bango:             %s\n", bango)
			}
		}
	} else {
		fmt.Println("Stage 2 Span: NOT FOUND")
	}

	// Stage 3 Report
	printSection("STAGE 3: DOMAIN PLANNING")
	if stage3Span != nil {
		fmt.Printf("Planner Selected: %s\n", stage3Span.StringAttr(telemetry.AttrStage3Planner))
		if stage3Span.Status.Code == "Error" || stage3Span.Status.Description != "" {
			fmt.Printf("Planner Error:    %s\n", stage3Span.Status.Description)
		} else {
			reasonsStr := stage3Span.StringAttr(telemetry.AttrStage3ActionReasonsJSON)
			if reasonsStr != "" {
				var reasons []stage3planner.ActionReason
				if err := json.Unmarshal([]byte(reasonsStr), &reasons); err == nil {
					fmt.Printf("Action Reasons (%d):\n", len(reasons))
					for _, r := range reasons {
						fmt.Printf("  - %s: %s\n", r.File, r.Reason)
					}
				}
			}
		}
	} else {
		fmt.Println("Stage 3 Span: NOT FOUND")
	}

	// Stage 4 Report
	printSection("STAGE 4: POST-PROCESS & SUBTITLE PAIRING")
	if stage4Span != nil {
		subtitlesCount := stage4Span.IntAttr(telemetry.AttrStage4SubtitlesPairedCount)
		if subtitlesCount > 0 {
			fmt.Printf("Subtitles Paired: %d\n", subtitlesCount)
		} else {
			fmt.Println("Subtitles Paired: (none)")
		}

		forcedSkipsStr := stage4Span.StringAttr(telemetry.AttrStage4ForcedSkipsJSON)
		if forcedSkipsStr != "" {
			var forcedSkips []string
			if err := json.Unmarshal([]byte(forcedSkipsStr), &forcedSkips); err == nil && len(forcedSkips) > 0 {
				fmt.Println("Forced Skips:")
				for _, fs := range forcedSkips {
					fmt.Printf("  - %s\n", fs)
				}
			}
		}
	} else {
		fmt.Println("Stage 4 Span: NOT FOUND")
	}

	// Final Result
	printSection("FINAL EXECUTION PLAN")
	if rootSpan != nil {
		if rootSpan.Status.Code == "Error" {
			fmt.Printf("RESULT: FAILED (%s)\n", rootSpan.Status.Description)
		} else {
			fmt.Printf("RESULT: SUCCESS (Took %.1fms)\n", rootSpan.DurationMs)
		}
	}

	if stage4Span != nil {
		planJSONStr := stage4Span.StringAttr(telemetry.AttrStage4FinalPlanJSON)
		if planJSONStr != "" {
			var finalPlan []model.PlanAction
			if err := json.Unmarshal([]byte(planJSONStr), &finalPlan); err == nil {
				fmt.Printf("Plan Actions (%d):\n", len(finalPlan))
				for i, a := range finalPlan {
					if a.Action == "move" && a.Target != nil {
						fmt.Printf("  [%2d] MOVE: %s\n       -->  %s\n", i+1, a.File, *a.Target)
					} else {
						fmt.Printf("  [%2d] SKIP: %s\n", i+1, a.File)
					}
				}
			}
		}
	}
	fmt.Println()
}

func printLiveFallbackReport(resp model.PlanResponse, planErr error) {
	printSection("FINAL EXECUTION PLAN")
	if planErr != nil {
		fmt.Printf("RESULT: FAILED (%v)\n", planErr)
	} else {
		fmt.Println("RESULT: SUCCESS")
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
