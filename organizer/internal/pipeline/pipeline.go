// Package pipeline orchestrates the 4-stage planning flow:
// Stage 1 classification -> Stage 2 metadata enrichment -> Stage 3 domain
// planning -> Stage 4 subtitle semantic pairing and physical security
// sanitization.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/model"
	stage1classifier "github.com/autoget-project/autoget/organizer/internal/pipeline/stage1_classifier"
	stage2enricher "github.com/autoget-project/autoget/organizer/internal/pipeline/stage2_enricher"
	stage3planner "github.com/autoget-project/autoget/organizer/internal/pipeline/stage3_planner"
	stage4postprocess "github.com/autoget-project/autoget/organizer/internal/pipeline/stage4_postprocess"
)

// Pipeline is the 4-stage planning orchestrator.
type Pipeline struct {
	provider  ai.Provider
	enricher  *stage2enricher.Enricher
	router    *stage3planner.Router
	subtitles *stage4postprocess.SubtitlePlanner
}

// NewPipeline wires the pipeline around the given AI provider and Stage 2
// enricher (both may be replaced by mocks in offline tests). tpdb may be nil,
// disabling the porn agent wiring in Stage 3.
func NewPipeline(provider ai.Provider, enricher *stage2enricher.Enricher, downloadDir, targetDir string, tpdb stage3planner.PornSource) *Pipeline {
	return &Pipeline{
		provider:  provider,
		enricher:  enricher,
		router:    stage3planner.NewRouter(provider, targetDir, tpdb),
		subtitles: stage4postprocess.NewSubtitlePlanner(provider, downloadDir),
	}
}

// CreatePlan runs the full 4-stage planning pipeline. Fatal system errors are
// returned as error (mapped to HTTP 500 by the handler layer); normal business
// degradation keeps the response error null (M6).
func (p *Pipeline) CreatePlan(ctx context.Context, dir string, files []string, metadata map[string]interface{}) (model.PlanResponse, error) {
	trace := TraceFromContext(ctx)
	if trace != nil {
		trace.StartTime = time.Now()
		trace.Dir = dir
		trace.Files = files
		trace.Metadata = metadata
	}

	if metaJSON, err := json.Marshal(metadata); err == nil {
		log.Printf("pipeline request: dir=%q files=%q metadata=%s", dir, files, metaJSON)
	} else {
		log.Printf("pipeline request: dir=%q files=%q metadata=%v (marshal error: %v)", dir, files, metadata, err)
	}

	// Stage 1: classification (rule screening first, LLM fallback).
	res, s1Detail, err := stage1classifier.ClassifyPipelineWithDetail(ctx, p.provider, files, metadata)
	if trace != nil {
		trace.Stage1.RuleMatched = !res.NeedLLM
		trace.Stage1.SearchContext = s1Detail.SearchContext
		trace.Stage1.Specialists = s1Detail.Specialists
		trace.Stage1.ArbiterUsed = s1Detail.ArbiterUsed
		trace.Stage1.ArbiterReason = s1Detail.ArbiterReason
		trace.Stage1.Category = res.Category
		trace.Stage1.Entities = res.Entities
	}
	if err != nil {
		if trace != nil {
			trace.Error = fmt.Sprintf("stage1 classification failed: %v", err)
			trace.DurationMs = time.Since(trace.StartTime).Milliseconds()
		}
		return model.PlanResponse{}, fmt.Errorf("stage1 classification failed: %w", err)
	}
	log.Printf("pipeline stage1 final: dir=%q files=%d category=%s", dir, len(files), res.Category)

	// Stage 2: metadata enrichment with graceful degradation (M6: never fatal).
	var enriched model.EnrichedMetadata
	if p.enricher != nil {
		enriched, err = p.enricher.Enrich(ctx, res.Category, files, metadata, res.Entities)
		if trace != nil {
			trace.Stage2.Enriched = enriched
			if err != nil {
				trace.Stage2.Err = err.Error()
			}
		}
		if err != nil {
			log.Printf("[M6 degrade] stage2 enrichment failed for %s: %v; continuing with local metadata", res.Category, err)
		}
	} else if trace != nil {
		trace.Stage2.Skipped = true
	}

	// Stage 3: domain planner routing.
	if trace != nil {
		trace.Stage3.PlannerName = string(res.Category)
	}
	actions, err := p.router.Plan(ctx, res.Category, &stage3planner.PlannerContext{
		Dir:      dir,
		Files:    files,
		Metadata: enriched,
		Entities: res.Entities,
	})
	if trace != nil {
		trace.Stage3.RawPlan = actions
		if err != nil {
			trace.Stage3.Err = err.Error()
		}
	}
	if err != nil {
		if trace != nil {
			trace.Error = fmt.Sprintf("stage3 planning failed for %s: %v", res.Category, err)
			trace.DurationMs = time.Since(trace.StartTime).Milliseconds()
		}
		return model.PlanResponse{}, fmt.Errorf("stage3 planning failed for %s: %w", res.Category, err)
	}

	// Stage 4a: companion subtitle semantic pairing for media categories.
	plan := actions
	if isMediaCategory(res.Category) {
		if subActions := p.pairSubtitles(ctx, dir, files, plan); len(subActions) > 0 {
			if trace != nil {
				trace.Stage4.SubtitlesPlanned = subActions
			}
			plan = append(plan, subActions...)
		}
	}

	// Stage 4b: physical security sanitization (traversal defense + garbage skip).
	finalPlan := stage4postprocess.SanitizePlan(plan)
	if trace != nil {
		trace.Stage4.SanitizedPlan = finalPlan
		trace.FinalPlan = finalPlan
		trace.DurationMs = time.Since(trace.StartTime).Milliseconds()
	}
	for i := range finalPlan {
		if finalPlan[i].Action == "move" && finalPlan[i].Target != nil {
			log.Printf("pipeline final plan: %q -> %q", finalPlan[i].File, *finalPlan[i].Target)
		} else {
			log.Printf("pipeline final plan: %q -> %s", finalPlan[i].File, finalPlan[i].Action)
		}
	}
	return model.PlanResponse{
		Plan:  finalPlan,
		Error: nil,
	}, nil
}

// pairSubtitles collects subtitle files left unplanned by Stage 3 and pairs
// them semantically; on LLM failure the video plan stays intact (M6 spirit).
func (p *Pipeline) pairSubtitles(ctx context.Context, dir string, files []string, plan []model.PlanAction) []model.PlanAction {
	planned := make(map[string]struct{}, len(plan))
	for _, a := range plan {
		planned[a.File] = struct{}{}
	}

	var subtitles []string
	for _, f := range files {
		if _, ok := planned[f]; ok {
			continue
		}
		if _, ok := model.SubtitleExtensions[strings.ToLower(filepath.Ext(f))]; ok {
			subtitles = append(subtitles, f)
		}
	}
	if len(subtitles) == 0 {
		return nil
	}

	subActions, err := p.subtitles.PairSubtitles(ctx, dir, subtitles, plan)
	if err != nil {
		log.Printf("[M6 degrade] stage4 subtitle pairing failed: %v; subtitles left unplanned", err)
		return nil
	}
	return subActions
}

// isMediaCategory reports whether the category produces video plans that can
// carry companion subtitles.
func isMediaCategory(cat model.Category) bool {
	switch cat {
	case model.CategoryMovie, model.CategoryTVSeries, model.CategoryBangoPorn, model.CategoryPorn:
		return true
	default:
		return false
	}
}
