package stage3planner

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/autoget-project/organizer/internal/model"
)

// logLLMPlanItems logs every file mapping returned by a planner LLM so
// regressions can be traced from the service logs.
func logLLMPlanItems(agent string, items []FilePlanItem) {
	for _, item := range items {
		log.Printf("stage3 %s llm: file=%q action=%q target=%q reason=%q", agent, item.File, item.Action, item.Target, item.Reason)
	}
}

// FilePlanItem is a single file mapping produced by a video planner LLM.
type FilePlanItem struct {
	File   string `json:"file"`
	Action string `json:"action"`
	Target string `json:"target,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// LLMPlanResponse is the strict structured output schema shared by the TV and
// movie planner LLMs.
type LLMPlanResponse struct {
	Plan []FilePlanItem `json:"plan"`
}

// partitionFiles splits the request files into videos, subtitles and others.
// Videos are planned by the domain LLM planners; subtitles are deferred to the
// Stage 4 semantic pairing; anything else is garbage and gets skipped.
func partitionFiles(files []string) (videos, subtitles, others []string) {
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		if _, ok := model.VideoExtensions[ext]; ok {
			videos = append(videos, f)
		} else if _, ok := model.SubtitleExtensions[ext]; ok {
			subtitles = append(subtitles, f)
		} else {
			others = append(others, f)
		}
	}
	return videos, subtitles, others
}

// ItemsToActions converts LLM file mappings into PlanActions preserving the
// input order. Input files never mentioned by the LLM are explicitly marked
// skip so every file is accounted for in the final plan.
func ItemsToActions(items []FilePlanItem, inputs []string) []model.PlanAction {
	byFile := make(map[string]FilePlanItem, len(items))
	for _, item := range items {
		if item.File == "" {
			continue
		}
		byFile[item.File] = item
	}

	actions := make([]model.PlanAction, 0, len(inputs))
	for _, f := range inputs {
		item, ok := byFile[f]
		if !ok {
			actions = append(actions, model.PlanAction{File: f, Action: "skip"})
			continue
		}
		if item.Action == "move" && item.Target != "" {
			target := filepath.Clean(item.Target)
			actions = append(actions, model.PlanAction{File: f, Action: "move", Target: &target})
			continue
		}
		actions = append(actions, model.PlanAction{File: f, Action: "skip"})
	}
	return actions
}

// skipOthers builds explicit skip actions for non-media garbage files.
func skipOthers(others []string) []model.PlanAction {
	actions := make([]model.PlanAction, 0, len(others))
	for _, o := range others {
		actions = append(actions, model.PlanAction{File: o, Action: "skip"})
	}
	return actions
}

// languageSegment resolves the language path segment, defaulting to "Others".
func languageSegment(meta model.EnrichedMetadata) string {
	if l := strings.TrimSpace(string(meta.Language)); l != "" {
		return l
	}
	return string(model.LanguageOthers)
}

// displayName resolves the best display title: the Stage 2 enriched title
// first, then the Stage 1 clean_title entity, then the provided fallback.
func displayName(meta model.EnrichedMetadata, entities map[string]interface{}, fallback string) string {
	if t := strings.TrimSpace(meta.Title); t != "" {
		return t
	}
	if entities != nil {
		if c, ok := entities["clean_title"].(string); ok && strings.TrimSpace(c) != "" {
			return strings.TrimSpace(c)
		}
	}
	return fallback
}

// firstStem returns the stem of the first file as a naming fallback.
func firstStem(files []string) string {
	if len(files) == 0 {
		return "Unknown"
	}
	base := filepath.Base(files[0])
	return strings.TrimSuffix(base, filepath.Ext(base))
}
