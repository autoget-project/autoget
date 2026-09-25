package pipeline

import (
	"context"
	"time"

	"github.com/autoget-project/autoget/organizer/internal/model"
	stage1classifier "github.com/autoget-project/autoget/organizer/internal/pipeline/stage1_classifier"
)

type traceContextKey struct{}

// StageTrace carries detailed, step-by-step diagnostic information for one pipeline run.
type StageTrace struct {
	StartTime  time.Time `json:"start_time"`
	DurationMs int64     `json:"duration_ms"`

	// Request inputs
	Dir      string                 `json:"dir"`
	Files    []string               `json:"files"`
	Metadata map[string]interface{} `json:"metadata"`

	// Stage 1: Classifier
	Stage1 struct {
		RuleMatched   bool                             `json:"rule_matched"`
		SearchContext stage1classifier.SearchContext   `json:"search_context,omitempty"`
		Specialists   []stage1classifier.CheckerResult `json:"specialists,omitempty"`
		ArbiterUsed   bool                             `json:"arbiter_used"`
		ArbiterReason string                           `json:"arbiter_reason,omitempty"`
		Category      model.Category                   `json:"category"`
		Entities      map[string]interface{}           `json:"entities,omitempty"`
	} `json:"stage1"`

	// Stage 2: Enricher
	Stage2 struct {
		Skipped  bool                   `json:"skipped"`
		Enriched model.EnrichedMetadata `json:"enriched"`
		Err      string                 `json:"err,omitempty"`
	} `json:"stage2"`

	// Stage 3: Domain Planner
	Stage3 struct {
		PlannerName string             `json:"planner_name"`
		RawPlan     []model.PlanAction `json:"raw_plan"`
		Err         string             `json:"err,omitempty"`
	} `json:"stage3"`

	// Stage 4: Post-Process & Subtitle Pairing
	Stage4 struct {
		SubtitlesPlanned []model.PlanAction `json:"subtitles_planned,omitempty"`
		SanitizedPlan    []model.PlanAction `json:"sanitized_plan"`
	} `json:"stage4"`

	FinalPlan []model.PlanAction `json:"final_plan"`
	Error     string             `json:"error,omitempty"`
}

// WithTraceCollector wraps ctx with a pointer to StageTrace so pipeline stages can record step info.
func WithTraceCollector(ctx context.Context, trace *StageTrace) context.Context {
	return context.WithValue(ctx, traceContextKey{}, trace)
}

// TraceFromContext returns the StageTrace pointer if present in context.
func TraceFromContext(ctx context.Context) *StageTrace {
	if v := ctx.Value(traceContextKey{}); v != nil {
		if t, ok := v.(*StageTrace); ok {
			return t
		}
	}
	return nil
}
