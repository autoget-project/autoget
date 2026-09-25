// Package pipeline orchestrates the 4-stage planning flow:
// Stage 1 classification -> Stage 2 metadata enrichment -> Stage 3 domain
// planning -> Stage 4 subtitle semantic pairing and physical security
// sanitization.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/model"
	stage1classifier "github.com/autoget-project/autoget/organizer/internal/pipeline/stage1_classifier"
	stage2enricher "github.com/autoget-project/autoget/organizer/internal/pipeline/stage2_enricher"
	stage3planner "github.com/autoget-project/autoget/organizer/internal/pipeline/stage3_planner"
	stage4postprocess "github.com/autoget-project/autoget/organizer/internal/pipeline/stage4_postprocess"
	"github.com/autoget-project/autoget/organizer/internal/telemetry"
)

// Pipeline is the 4-stage planning orchestrator.
type Pipeline struct {
	provider  ai.Provider
	enricher  *stage2enricher.Enricher
	router    *stage3planner.Router
	subtitles *stage4postprocess.SubtitlePlanner
	tracer    trace.Tracer
}

// NewPipeline wires the pipeline around the given AI provider and Stage 2
// enricher (both may be replaced by mocks in offline tests). tpdb may be nil,
// disabling the porn agent wiring in Stage 3. tracer may be nil, in which case
// it defaults to telemetry.Tracer().
func NewPipeline(provider ai.Provider, enricher *stage2enricher.Enricher, downloadDir, targetDir string, tpdb stage3planner.PornSource, tracer trace.Tracer) *Pipeline {
	if tracer == nil {
		tracer = telemetry.Tracer()
	}
	return &Pipeline{
		provider:  provider,
		enricher:  enricher,
		router:    stage3planner.NewRouter(provider, targetDir, tpdb),
		subtitles: stage4postprocess.NewSubtitlePlanner(provider, downloadDir),
		tracer:    tracer,
	}
}

// CreatePlan runs the full 4-stage planning pipeline. Fatal system errors are
// returned as error (mapped to HTTP 500 by the handler layer); normal business
// degradation keeps the response error null (M6).
func (p *Pipeline) CreatePlan(ctx context.Context, dir string, files []string, metadata map[string]interface{}) (model.PlanResponse, error) {
	tr := p.tracer
	if tr == nil {
		tr = telemetry.Tracer()
	}
	spanAttrs := []attribute.KeyValue{
		attribute.String(telemetry.AttrOrganizerDir, dir),
		attribute.Int(telemetry.AttrOrganizerFilesCount, len(files)),
	}
	if len(files) > 0 {
		spanAttrs = append(spanAttrs, attribute.StringSlice(telemetry.AttrOrganizerFiles, files))
	}
	if len(metadata) > 0 {
		if mJSON, jerr := json.Marshal(metadata); jerr == nil {
			const maxMetadataBytes = 16 * 1024
			mStr := string(mJSON)
			if len(mStr) > maxMetadataBytes {
				mStr = mStr[:maxMetadataBytes] + "...(truncated)"
			}
			spanAttrs = append(spanAttrs, attribute.String(telemetry.AttrOrganizerMetadataJSON, mStr))
		}
	}
	ctx, rootSpan := tr.Start(ctx, telemetry.SpanPipelineCreatePlan,
		trace.WithAttributes(spanAttrs...),
	)
	defer rootSpan.End()

	// Stage 1: classification (rule screening first, LLM fallback).
	ctxStage1, spanStage1 := tr.Start(ctx, telemetry.SpanStage1Classify)
	res, s1Detail, err := stage1classifier.ClassifyPipelineWithDetail(ctxStage1, p.provider, files, metadata)
	spanStage1.SetAttributes(
		attribute.Bool(telemetry.AttrStage1RuleMatched, !res.NeedLLM),
		attribute.String(telemetry.AttrStage1Category, string(res.Category)),
		attribute.Bool(telemetry.AttrStage1ArbiterUsed, s1Detail.ArbiterUsed),
	)
	if s1Detail.ArbiterUsed && s1Detail.ArbiterReason != "" {
		spanStage1.SetAttributes(attribute.String(telemetry.AttrStage1ArbiterReason, s1Detail.ArbiterReason))
	}
	if len(s1Detail.Specialists) > 0 {
		if specJSON, jsonErr := json.Marshal(s1Detail.Specialists); jsonErr == nil {
			spanStage1.SetAttributes(attribute.String(telemetry.AttrStage1SpecialistsJSON, string(specJSON)))
		}
	}
	if s1Detail.SearchContext.HasInfo() {
		if scJSON, jsonErr := json.Marshal(s1Detail.SearchContext); jsonErr == nil {
			spanStage1.SetAttributes(attribute.String(telemetry.AttrStage1SearchContextJSON, string(scJSON)))
		}
	}
	if err != nil {
		spanStage1.RecordError(err)
		spanStage1.SetStatus(codes.Error, err.Error())
		spanStage1.End()

		rootSpan.RecordError(err)
		rootSpan.SetStatus(codes.Error, err.Error())

		return model.PlanResponse{}, fmt.Errorf("stage1 classification failed: %w", err)
	}
	spanStage1.End()

	// Stage 2: metadata enrichment with graceful degradation (M6: never fatal).
	ctxStage2, spanStage2 := tr.Start(ctx, telemetry.SpanStage2Enrich)
	var enriched model.EnrichedMetadata
	if p.enricher != nil {
		var s2Detail stage2enricher.EnricherDetail
		enriched, s2Detail, _ = p.enricher.EnrichWithDetail(ctxStage2, res.Category, files, metadata, res.Entities)
		spanStage2.SetAttributes(
			attribute.Bool(telemetry.AttrStage2Skipped, false),
			attribute.String(telemetry.AttrStage2EnrichedTitle, enriched.Title),
			attribute.Int(telemetry.AttrStage2EnrichedYear, enriched.Year),
			attribute.String(telemetry.AttrStage2EnrichedBango, enriched.Bango),
		)
		for _, warn := range s2Detail.DegradeWarnings {
			spanStage2.AddEvent("stage2_degraded", trace.WithAttributes(
				attribute.String("warning", warn),
			))
		}
	} else {
		spanStage2.SetAttributes(attribute.Bool(telemetry.AttrStage2Skipped, true))
	}
	spanStage2.End()

	// Stage 3: domain planner routing.
	ctxStage3, spanStage3 := tr.Start(ctx, telemetry.SpanStage3Plan)
	spanStage3.SetAttributes(attribute.String(telemetry.AttrStage3Planner, string(res.Category)))
	actions, s3Detail, err := p.router.PlanWithDetail(ctxStage3, res.Category, &stage3planner.PlannerContext{
		Dir:      dir,
		Files:    files,
		Metadata: enriched,
		Entities: res.Entities,
	})
	spanStage3.SetAttributes(attribute.Int(telemetry.AttrStage3RawActionsCount, len(actions)))
	if len(s3Detail.ActionReasons) > 0 {
		if rJSON, jsonErr := json.Marshal(s3Detail.ActionReasons); jsonErr == nil {
			spanStage3.SetAttributes(attribute.String(telemetry.AttrStage3ActionReasonsJSON, string(rJSON)))
		}
	}
	if err != nil {
		spanStage3.RecordError(err)
		spanStage3.SetStatus(codes.Error, err.Error())
		spanStage3.End()

		rootSpan.RecordError(err)
		rootSpan.SetStatus(codes.Error, err.Error())

		return model.PlanResponse{}, fmt.Errorf("stage3 planning failed for %s: %w", res.Category, err)
	}
	spanStage3.End()

	// Stage 4: companion subtitle semantic pairing and physical security sanitization.
	ctxStage4, spanStage4 := tr.Start(ctx, telemetry.SpanStage4PostProcess)
	plan := actions
	subtitlesPairedCount := 0
	if isMediaCategory(res.Category) {
		if subActions := p.pairSubtitles(ctxStage4, dir, files, plan); len(subActions) > 0 {
			subtitlesPairedCount = len(subActions)
			plan = append(plan, subActions...)
		}
	}

	finalPlan, s4Detail := stage4postprocess.SanitizePlanWithDetail(plan)
	spanStage4.SetAttributes(
		attribute.Int(telemetry.AttrStage4SubtitlesPairedCount, subtitlesPairedCount),
		attribute.Int(telemetry.AttrStage4FinalActionsCount, len(finalPlan)),
	)
	if len(s4Detail.ForcedSkips) > 0 {
		if skipsJSON, jsonErr := json.Marshal(s4Detail.ForcedSkips); jsonErr == nil {
			spanStage4.SetAttributes(attribute.String(telemetry.AttrStage4ForcedSkipsJSON, string(skipsJSON)))
		}
	}
	if planJSON, jsonErr := json.Marshal(finalPlan); jsonErr == nil {
		spanStage4.SetAttributes(attribute.String(telemetry.AttrStage4FinalPlanJSON, string(planJSON)))
	}
	spanStage4.End()

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
