package stage3planner

import (
	"context"
	"fmt"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/model"
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

// ActionReason records why a single plan action was produced.
type ActionReason struct {
	File   string `json:"file"`
	Reason string `json:"reason,omitempty"`
}

// PlannerDetail captures diagnostic details from Stage 3 domain planning.
type PlannerDetail struct {
	Planner       string         `json:"planner"`
	ActionReasons []ActionReason `json:"action_reasons,omitempty"`
}

// Plan routes the request to the planner matching the category.
func (r *Router) Plan(ctx context.Context, cat model.Category, pc *PlannerContext) ([]model.PlanAction, error) {
	actions, _, err := r.PlanWithDetail(ctx, cat, pc)
	return actions, err
}

// PlanWithDetail routes the request to the domain planner matching the category:
//   - tv_series -> TVPlanner (LLM-based Jellyfin naming)
//   - movie -> MoviePlanner (LLM-based feature extraction and Jellyfin naming)
//   - bango_porn -> BangoPlanner (decision matrix + LLM filename canonicalization)
//   - porn -> PornPlanner (ThePornDB tool agent with local fallback chain)
//   - {photobook, audio_book, book, music, music_video} -> SimplePlan (local deterministic move)
//   - unknown -> empty plan and nil error
//
// It returns the planned actions, a PlannerDetail with per-action reasons (when provided
// by LLM planners), and any error.
func (r *Router) PlanWithDetail(ctx context.Context, cat model.Category, pc *PlannerContext) ([]model.PlanAction, PlannerDetail, error) {
	if pc == nil {
		return nil, PlannerDetail{}, fmt.Errorf("planner context is nil")
	}

	detail := PlannerDetail{
		Planner: string(cat),
	}

	switch cat {
	case model.CategoryTVSeries:
		items, err := r.tv.PlanItems(ctx, pc)
		if err != nil {
			return nil, detail, err
		}
		detail.ActionReasons = reasonsFromItems(items)
		actions := buildActionsFromItems(items, pc.Files)
		return actions, detail, nil
	case model.CategoryMovie:
		items, err := r.movie.PlanItems(ctx, pc)
		if err != nil {
			return nil, detail, err
		}
		detail.ActionReasons = reasonsFromItems(items)
		actions := buildActionsFromItems(items, pc.Files)
		return actions, detail, nil
	case model.CategoryBangoPorn:
		actions, err := r.bango.Plan(ctx, pc)
		return actions, detail, err
	case model.CategoryPorn:
		actions, err := r.porn.Plan(ctx, pc)
		return actions, detail, err
	default:
		if IsSimpleMoveCategory(cat) {
			return SimplePlan(cat, pc.Files), detail, nil
		}
		// unknown: empty plan and nil error (normal planning outcome).
		return nil, detail, nil
	}
}

func reasonsFromItems(items []FilePlanItem) []ActionReason {
	out := make([]ActionReason, 0, len(items))
	for _, it := range items {
		if it.Reason == "" {
			continue
		}
		out = append(out, ActionReason{File: it.File, Reason: it.Reason})
	}
	return out
}

func buildActionsFromItems(items []FilePlanItem, files []string) []model.PlanAction {
	videos, _, others := partitionFiles(files)
	actions := ItemsToActions(items, videos)
	for _, o := range others {
		actions = append(actions, model.PlanAction{File: o, Action: "skip"})
	}
	return actions
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
