package stage3planner

import (
	"context"
	"fmt"

	"github.com/autoget-project/organizer/internal/ai"
	"github.com/autoget-project/organizer/internal/model"
)

// PlannerContext carries the inputs required by Stage 3 domain planners.
type PlannerContext struct {
	// Dir is the download sub-directory of the current request (consumed by
	// Stage 4 subtitle pairing).
	Dir string
	// Files lists the raw relative file paths from the upstream request.
	Files []string
	// Metadata is the Stage 2 enriched metadata.
	Metadata model.EnrichedMetadata
	// Entities carries Stage 1 extracted entities (e.g. clean_title, bango, id).
	Entities map[string]interface{}
}

// Planner is the interface implemented by every Stage 3 domain planner.
type Planner interface {
	Plan(ctx context.Context, pc *PlannerContext) ([]model.PlanAction, error)
}

// Router dispatches planning to the domain planner matching the Stage 1 category.
type Router struct {
	tv    *TVPlanner
	movie *MoviePlanner
	bango *BangoPlanner
	porn  *PornPlanner
}

// NewRouter creates a Router with all domain planners wired to the given
// provider; porn uses the local planner with optional tpdb agent wiring
// (agent lands later).
func NewRouter(provider ai.Provider, targetDir string, tpdb PornSource) *Router {
	return &Router{
		tv:    NewTVPlanner(provider),
		movie: NewMoviePlanner(provider),
		bango: NewBangoPlanner(provider),
		porn:  NewPornPlanner(provider, targetDir, tpdb),
	}
}

// Plan routes the request to the planner matching the category:
//   - tv_series / movie / bango_porn -> dedicated LLM planners;
//   - porn -> local Porn Planner (naming fallback chain);
//   - simple categories -> local Simple Planner three-branch strategy;
//   - unknown -> empty plan with nil error.
func (r *Router) Plan(ctx context.Context, cat model.Category, pc *PlannerContext) ([]model.PlanAction, error) {
	if pc == nil {
		return nil, fmt.Errorf("planner context is nil")
	}

	switch cat {
	case model.CategoryTVSeries:
		return r.tv.Plan(ctx, pc)
	case model.CategoryMovie:
		return r.movie.Plan(ctx, pc)
	case model.CategoryBangoPorn:
		return r.bango.Plan(ctx, pc)
	case model.CategoryPorn:
		return r.porn.Plan(ctx, pc)
	default:
		if IsSimpleMoveCategory(cat) {
			return SimplePlan(cat, pc.Files), nil
		}
		// unknown: empty plan and nil error (normal planning outcome).
		return nil, nil
	}
}

// IsSimpleMoveCategory reports whether the category belongs to the simple
// move set {photobook, audio_book, book, music, music_video}.
func IsSimpleMoveCategory(cat model.Category) bool {
	for _, c := range model.SimpleMoveCategories {
		if c == cat {
			return true
		}
	}
	return false
}
