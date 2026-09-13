package stage3planner

import (
	"context"
	"encoding/json"
	"fmt"
	"path"

	"github.com/autoget-project/organizer/internal/ai"
	"github.com/autoget-project/organizer/internal/model"
)

// tvTargetRoot resolves the tv_series root dir based on the animation flag (H4).
func tvTargetRoot(isAnim bool) model.TargetDir {
	if isAnim {
		return model.TargetDirAnimTVSeries
	}
	return model.TargetDirTVSeries
}

// TVPlanner plans tv_series downloads by delegating messy season/episode name
// mapping to the LLM. Animation series are routed to the anim_tv_series root
// (H4); no fragile episode regexes are ever applied in Go code.
type TVPlanner struct {
	provider ai.Provider
}

// NewTVPlanner creates a new TVPlanner.
func NewTVPlanner(provider ai.Provider) *TVPlanner {
	return &TVPlanner{provider: provider}
}

// Plan generates the Jellyfin-compatible move plan for TV series videos.
func (p *TVPlanner) Plan(ctx context.Context, pc *PlannerContext) ([]model.PlanAction, error) {
	videos, _, others := partitionFiles(pc.Files)
	if len(videos) == 0 {
		// No videos: skip the wasted LLM call; garbage files stay skip actions
		// and subtitles are handled by Stage 4.
		return skipOthers(others), nil
	}

	root := tvTargetRoot(pc.Metadata.IsAnim)
	lang := languageSegment(pc.Metadata)
	rootPath := path.Join(string(root), lang)

	input := map[string]interface{}{
		"root_path": rootPath,
		"files":     videos,
		"tv_series": map[string]interface{}{
			"name":     displayName(pc.Metadata, pc.Entities, firstStem(videos)),
			"year":     pc.Metadata.Year,
			"language": lang,
			"is_anim":  pc.Metadata.IsAnim,
		},
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("tv planner failed to marshal input: %w", err)
	}

	var resp LLMPlanResponse
	prompt := fmt.Sprintf(tvPlannerPrompt, string(payload))
	if err := p.provider.GenerateStructured(ctx, prompt, LLMPlanResponse{}, &resp); err != nil {
		return nil, fmt.Errorf("tv planner llm generation failed: %w", err)
	}
	logLLMPlanItems("tv", resp.Plan)

	actions := ItemsToActions(resp.Plan, videos)
	for _, o := range others {
		actions = append(actions, model.PlanAction{File: o, Action: "skip"})
	}
	return actions, nil
}

const tvPlannerPrompt = `Task: You are an AI system that organizes TV series downloads into Jellyfin's preferred folder and file naming conventions.

You must analyze the downloaded video files along with the provided TV series metadata, and produce a JSON plan describing how each file should be renamed and moved.

Specifics:
1. Input:
   - JSON object containing:
     - "root_path": the mandatory root prefix of every move target.
     - "files": array of video file paths. Names may contain release-group noise and unusual episode labels such as "第03话", "1x05", "[01-02合集]", "OVA1", "SP02", "E01v2".
     - "tv_series": object containing:
       - "name": the TV series display name (Simplified Chinese preferred when available)
       - "year": the first season release year
       - "language": the language name used inside paths
       - "is_anim": whether it is an animation
2. Analyze:
   - Use the provided TV series metadata instead of searching.
   - Semantically parse messy filenames to extract season and episode numbers; never rely on a single rigid pattern:
     "第03话" -> S01E03; "1x05" -> S01E05; "[01-02合集]" -> consecutive episodes mapped to separate SxxEyy entries; "OVA1"/"SP02" -> Season 00 specials; "E01v2" -> S01E01 (v2 is just a release version tag).
   - Ignore release-group noise like [HDSky], 1080p.WEB-DL, x264, DDP5.1, Atmos.
3. Construct new Jellyfin-compatible relative paths:
   - Folder: {root_path}/<Series Name (Year)>/Season XX
   - Video:  {root_path}/<Series Name (Year)>/Season XX/<Series Name (Year)> SXXEYY.ext
   - Use two-digit zero-padded season and episode numbers (Season 00 for specials/OVA).
   - Use the series name from metadata; preserve the original file extension.
4. Edge cases:
   - If the video is an extra, an unwanted duplicate version, or cannot be matched to the series: "action": "skip" (omit target).
   - Every input file must appear exactly once in the plan.

Respond strictly following the required JSON schema.

Input:
%s`
