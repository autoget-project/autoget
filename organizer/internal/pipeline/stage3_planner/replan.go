package stage3planner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/model"
)

// ReplanInput is the input of a replan planning call. The previous plan and the
// user hint are dedicated fields, never merged into Metadata: a replan only
// exists because the previous result was flawed, so it must not be mistaken for
// authoritative upstream metadata. Enriched carries the Stage 2 facts (TMDB /
// MetaTube / ActorStore), which the replan prompt treats as authoritative.
type ReplanInput struct {
	Files        []string
	Metadata     map[string]interface{}
	Entities     map[string]interface{}
	Enriched     model.EnrichedMetadata
	PreviousPlan []model.PlanAction
	UserHint     string
}

// PlanReplan runs the replan LLM matching cat and assembles the final action
// list: videos come from the LLM, companion subtitles are left to Stage 4
// pairing, and any other files are explicitly skipped.
func PlanReplan(ctx context.Context, provider ai.Provider, cat model.Category, in ReplanInput) ([]model.PlanAction, error) {
	videos, _, others := partitionFiles(in.Files)

	// The LLM only names videos; subtitles are Stage 4's concern.
	llmInput := in
	llmInput.Files = videos

	var resp LLMPlanResponse
	if err := provider.GenerateStructured(ctx, ReplanPrompt(cat, llmInput), LLMPlanResponse{}, &resp); err != nil {
		return nil, fmt.Errorf("replan llm generation failed: %w", err)
	}

	actions := ItemsToActions(resp.Plan, videos)
	return append(actions, skipOthers(others)...), nil
}

// ReplanPrompt builds the replan prompt for cat. Categories with a dedicated
// domain prompt (tv_series, movie, bango_porn, porn) get the domain-specific
// template plus the mandatory root prefix; other categories fall back to the
// generic replan prompt.
func ReplanPrompt(cat model.Category, in ReplanInput) string {
	payload := map[string]interface{}{
		"files":         in.Files,
		"metadata":      in.Metadata,
		"enriched":      in.Enriched,
		"previous_plan": in.PreviousPlan,
		"user_hint":     in.UserHint,
	}
	if len(in.Entities) > 0 {
		payload["entities"] = in.Entities
	}

	var tpl string
	switch cat {
	case model.CategoryTVSeries:
		tpl = tvReplanPrompt
		payload["root_path"] = string(model.TargetDirTVSeries)
	case model.CategoryMovie:
		tpl = movieReplanPrompt
		payload["root_path"] = string(model.TargetDirMovie)
	case model.CategoryBangoPorn:
		tpl = bangoReplanPrompt
		payload["root_path"] = string(model.TargetDirJAV)
		// The canonical actress directory (ActorStore-resolved, 素人 when
		// unknown) is a Go decision, exactly like the normal bango planner's
		// target_dir: the LLM must not invent its own transliteration.
		payload["actor_dir"] = BangoActorDir(in.Enriched)
	case model.CategoryPorn:
		tpl = pornReplanPrompt
		payload["root_path"] = string(model.TargetDirPorn)
	default:
		tpl = genericReplanPrompt
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(tpl, "{}")
	}
	return fmt.Sprintf(tpl, string(data))
}

const replanCommonRules = `1. Input (JSON object):
   - "root_path" (present for known library domains): the mandatory root prefix of every move target.
   - "files": the original array of file paths.
   - "enriched": authoritative metadata resolved by the pipeline (official title, year, language, is_anim, bango, actors, maker, is_vr, from_madou). Prefer these over your own reading of the filename or the raw metadata.
   - "metadata": upstream indexer metadata (raw clues such as title, dmm_id, actors, labels).
   - "entities": facts extracted during re-classification (clean_title, year, actors, bango, dmm_id, is_vr).
   - "previous_plan": the previous, FLAWED plan. Do NOT trust or copy it blindly; the user requested a replan precisely because it was wrong. Use it only to understand what was previously attempted.
   - "user_hint": the user's correction. It may be empty when the user asked for a plain replan.
2. Analyze:
   - Re-derive the correct naming from the files, enriched, metadata, entities and user_hint.
   - Apply the user_hint exactly when present; when it conflicts with the previous plan, the hint wins.
   - Override every part of the previous plan that the hint or your own analysis identifies as wrong (including the category/library root).`

const replanResponseRules = `Respond strictly following the required JSON schema.

Input:
%s`

const tvReplanPrompt = `Task: You are an AI system that revises a TV series file organization plan based on user feedback.

A user requested this replan, so the previous plan is known to be flawed. Re-derive a corrected plan instead of preserving the previous one.

` + replanCommonRules + `
3. Construct new Jellyfin-compatible relative paths:
   - Folder: {root_path}/{Lang}/<Series Name (Year)>/Season XX
   - Video:  {root_path}/{Lang}/<Series Name (Year)>/Season XX/<Series Name (Year)> SXXEYY.ext
   - Every move target must start with the mandatory root prefix.
4. Edge cases:
   - Extras, samples or unmatchable files: "action": "skip" (omit target).
   - Every input file must appear exactly once in the plan.

` + replanResponseRules

const movieReplanPrompt = `Task: You are an AI system that revises a movie file organization plan based on user feedback.

A user requested this replan, so the previous plan is known to be flawed. Re-derive a corrected plan instead of preserving the previous one.

` + replanCommonRules + `
3. Construct new Jellyfin-compatible relative paths:
   - Folder: {root_path}/{Lang}/<Movie Name (Year)>
   - Video:  {root_path}/{Lang}/<Movie Name (Year)>/<Movie Name (Year)>.ext
   - Every move target must start with the mandatory root prefix.
4. Edge cases:
   - Samples, trailers and extras: "action": "skip" (omit target).
   - Every input file must appear exactly once in the plan.

` + replanResponseRules

const bangoReplanPrompt = `Task: You are an AI system that revises a bango (JAV) file organization plan based on user feedback.

A user requested this replan, so the previous plan is known to be flawed. Re-derive a corrected plan instead of preserving the previous one.

` + replanCommonRules + `
3. Construct new relative paths following the JAV library convention:
   - The actress directory layer MUST be exactly "actor_dir" from the input (the canonical library directory already resolved by the actress store), never your own transliteration of the performer name.
   - {root_path}/{actor_dir}/<BANGO>.part.N.ext for multi-volume files; <BANGO>-C.ext keeps the Chinese subtitle marker.
   - Every move target must start with the mandatory root prefix.
4. Edge cases:
   - Extras and unmatchable files: "action": "skip" (omit target).
   - Every input file must appear exactly once in the plan.

` + replanResponseRules

const pornReplanPrompt = `Task: You are an AI system that revises a western/general adult video (porn) file organization plan based on user feedback.

A user requested this replan, so the previous plan is known to be flawed. Re-derive a corrected plan instead of preserving the previous one.

` + replanCommonRules + `
3. Construct new relative paths following the porn library convention:
   - {root_path}/<Scene Title>/<Scene Title>.ext
   - Multi-part scenes of the same title: <Scene Title>.part.N.ext
   - Every move target must start with the mandatory root prefix.
4. Edge cases:
   - Extras and unmatchable files: "action": "skip" (omit target).
   - Every input file must appear exactly once in the plan.

` + replanResponseRules

const genericReplanPrompt = `Task: You are an AI system that revises file organization plans based on user feedback.

A user requested this replan, so the previous plan is known to be flawed. Re-derive a corrected plan instead of preserving the previous one.

` + replanCommonRules + `
3. Generate an improved plan:
   - Every move target must be a valid relative path that never escapes the media library.
4. Response format:
   - Return a plan containing one action per file.
   - Each action must be either "move" (with a target path) or "skip" (omit target).
5. Edge cases:
   - If the user hint is unclear, make reasonable assumptions.
   - Every input file must appear exactly once in the plan.

` + replanResponseRules
