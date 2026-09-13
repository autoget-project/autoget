package stage3planner

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	"github.com/autoget-project/organizer/internal/ai"
	"github.com/autoget-project/organizer/internal/model"
)

// movieTargetRoot resolves the movie root dir based on the animation flag (H4).
func movieTargetRoot(isAnim bool) model.TargetDir {
	if isAnim {
		return model.TargetDirAnimMovie
	}
	return model.TargetDirMovie
}

// MoviePlanner plans movie downloads via LLM: it identifies the main feature
// among samples/trailers/extras and applies Jellyfin naming. Animation movies
// are routed to the anim_movie root (H4).
type MoviePlanner struct {
	provider ai.Provider
}

// NewMoviePlanner creates a new MoviePlanner.
func NewMoviePlanner(provider ai.Provider) *MoviePlanner {
	return &MoviePlanner{provider: provider}
}

// Plan generates the Jellyfin-compatible move plan for movie videos.
func (p *MoviePlanner) Plan(ctx context.Context, pc *PlannerContext) ([]model.PlanAction, error) {
	videos, _, others := partitionFiles(pc.Files)
	if len(videos) == 0 {
		// No videos: skip the wasted LLM call; garbage files stay skip actions
		// and subtitles are handled by Stage 4.
		return skipOthers(others), nil
	}

	root := movieTargetRoot(pc.Metadata.IsAnim)
	lang := languageSegment(pc.Metadata)
	rootPath := path.Join(string(root), lang)

	input := map[string]interface{}{
		"root_path": rootPath,
		"files":     videos,
		"movie": map[string]interface{}{
			"name":     displayName(pc.Metadata, pc.Entities, firstStem(videos)),
			"year":     pc.Metadata.Year,
			"language": lang,
			"is_anim":  pc.Metadata.IsAnim,
		},
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("movie planner failed to marshal input: %w", err)
	}

	var resp LLMPlanResponse
	prompt := fmt.Sprintf(moviePlannerPrompt, string(payload))
	if err := p.provider.GenerateStructured(ctx, prompt, LLMPlanResponse{}, &resp); err != nil {
		return nil, fmt.Errorf("movie planner llm generation failed: %w", err)
	}
	logLLMPlanItems("movie", resp.Plan)

	actions := ItemsToActions(resp.Plan, videos)
	for _, o := range others {
		actions = append(actions, model.PlanAction{File: o, Action: "skip"})
	}
	return actions, nil
}

const moviePlannerPrompt = `Task: You are an AI system that organizes movie downloads into Jellyfin's preferred folder and file naming conventions.

You must analyze the downloaded video files along with the provided movie metadata, and produce a JSON plan describing how each file should be renamed and moved.

Specifics:
1. Input:
   - JSON object containing:
     - "root_path": the mandatory root prefix of every move target.
     - "files": array of video file paths, possibly mixed with samples, trailers and other extras.
     - "movie": object containing:
       - "name": the movie display name (Simplified Chinese preferred when available)
       - "year": the release year
       - "language": the language name used inside paths
       - "is_anim": whether it is an animation
2. Analyze:
   - Use the provided movie metadata instead of searching.
   - Identify the main feature video(s); ignore release-group noise like [HDSky], 1080p.WEB-DL, x264, DDP5.1, Atmos.
   - Recognize samples, trailers, behind-the-scenes clips and advertising videos by name or semantics.
3. Construct new Jellyfin-compatible relative paths:
   - Folder: {root_path}/<Movie Name (Year)>
   - Video:  {root_path}/<Movie Name (Year)>/<Movie Name (Year)>.ext
   - Use the movie name from metadata; preserve the original file extension.
4. Edge cases:
   - Samples, trailers, extras and non-feature videos: "action": "skip" (omit target).
   - If multiple separate movies are detected, group them into separate logical folders.
   - Every input file must appear exactly once in the plan.

Respond strictly following the required JSON schema.

Input:
%s`
