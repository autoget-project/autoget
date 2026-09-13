package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/autoget-project/organizer/internal/ai"
	"github.com/autoget-project/organizer/internal/model"
	stage3planner "github.com/autoget-project/organizer/internal/pipeline/stage3_planner"
	stage4postprocess "github.com/autoget-project/organizer/internal/pipeline/stage4_postprocess"
)

// replanDomain identifies the Stage 3 domain an existing plan belongs to.
type replanDomain string

const (
	domainTV    replanDomain = "tv_series"
	domainMovie replanDomain = "movie"
	domainBango replanDomain = "bango_porn"
)

// targetRootToDomain maps known category root directories (the first segment
// of every move target) to their Stage 3 LLM domain.
var targetRootToDomain = map[string]replanDomain{
	string(model.TargetDirTVSeries):     domainTV,
	string(model.TargetDirAnimTVSeries): domainTV,
	string(model.TargetDirMovie):        domainMovie,
	string(model.TargetDirAnimMovie):    domainMovie,
	string(model.TargetDirJAV):          domainBango,
	string(model.TargetDirJAVVR):        domainBango,
	string(model.TargetDirMadou):        domainBango,
}

// ReplanHandler serves POST /v1/replan-with-hint.
type ReplanHandler struct {
	provider ai.Provider
}

// NewReplanHandler creates a new ReplanHandler.
func NewReplanHandler(provider ai.Provider) *ReplanHandler {
	return &ReplanHandler{provider: provider}
}

// Handle performs a single replan driven by the user hint. Stage 1
// classification and Stage 2 metadata retrieval are never re-run. The domain
// is inferred from the first move target of the previous plan (L14): known
// category roots route to the matching Stage 3 domain LLM replan; anything
// else (empty plan or unknown root) falls back to the generic replan prompt.
func (h *ReplanHandler) Handle(w http.ResponseWriter, r *http.Request) {
	var req model.APIReplanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	items, err := h.callReplanLLM(r.Context(), req)
	if err != nil {
		msg := err.Error()
		writeJSON(w, http.StatusInternalServerError, model.PlanResponse{Plan: []model.PlanAction{}, Error: &msg})
		return
	}

	plan := stage3planner.ItemsToActions(items, req.Files)
	writeJSON(w, http.StatusOK, model.PlanResponse{
		Plan:  stage4postprocess.SanitizePlan(plan),
		Error: nil,
	})
}

// callReplanLLM routes the request to the inferred domain replan prompt or the
// generic fallback and returns the strict structured LLM response.
func (h *ReplanHandler) callReplanLLM(ctx context.Context, req model.APIReplanRequest) ([]stage3planner.FilePlanItem, error) {
	var prompt string
	if domain, ok := inferDomainFromPreviousPlan(req.PreviousResponse.Plan); ok {
		payload, err := json.Marshal(map[string]interface{}{
			"root_path":     domainRoot(domain),
			"files":         req.Files,
			"previous_plan": req.PreviousResponse.Plan,
			"user_hint":     req.UserHint,
		})
		if err != nil {
			return nil, fmt.Errorf("replan failed to marshal input: %w", err)
		}
		prompt = fmt.Sprintf(domainReplanPrompt(domain), string(payload))
	} else {
		// Fallback: generic replan prompt (revises the whole plan based on
		// the user hint).
		payload, err := json.Marshal(map[string]interface{}{
			"original_request":  map[string]interface{}{"files": req.Files, "metadata": req.Metadata},
			"previous_response": req.PreviousResponse,
			"user_hint":         req.UserHint,
		})
		if err != nil {
			return nil, fmt.Errorf("replan failed to marshal input: %w", err)
		}
		prompt = fmt.Sprintf(genericReplanPrompt, string(payload))
	}

	var resp stage3planner.LLMPlanResponse
	if err := h.provider.GenerateStructured(ctx, prompt, stage3planner.LLMPlanResponse{}, &resp); err != nil {
		return nil, fmt.Errorf("replan llm generation failed: %w", err)
	}
	for _, item := range resp.Plan {
		log.Printf("replan llm: file=%q action=%q target=%q reason=%q", item.File, item.Action, item.Target, item.Reason)
	}
	return resp.Plan, nil
}

// inferDomainFromPreviousPlan inspects the first move action of the previous
// plan and maps its target root directory to a known domain (L14).
func inferDomainFromPreviousPlan(plan []model.PlanAction) (replanDomain, bool) {
	for _, a := range plan {
		if a.Action != "move" || a.Target == nil {
			continue
		}
		cleaned := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(*a.Target)), "./")
		if cleaned == "." || cleaned == ".." {
			return "", false
		}
		root := cleaned
		if idx := strings.Index(cleaned, "/"); idx >= 0 {
			root = cleaned[:idx]
		}
		domain, ok := targetRootToDomain[root]
		if !ok {
			return "", false
		}
		return domain, true
	}
	return "", false
}

// domainRoot returns the mandatory root prefix for the given domain.
// Deliberate naming convention: all bango-family roots (jav/, jav_vr/,
// madou/) are coerced to the canonical "jav/" root in replan prompts — the
// replan LLM only needs the library-family prefix, and the previous plan's
// exact root is already visible to it via previous_plan.
func domainRoot(domain replanDomain) string {
	switch domain {
	case domainTV:
		return string(model.TargetDirTVSeries)
	case domainMovie:
		return string(model.TargetDirMovie)
	case domainBango:
		return string(model.TargetDirJAV)
	default:
		return ""
	}
}

// domainReplanPrompt returns the domain-specific replan prompt template.
func domainReplanPrompt(domain replanDomain) string {
	switch domain {
	case domainTV:
		return tvReplanPrompt
	case domainMovie:
		return movieReplanPrompt
	case domainBango:
		return bangoReplanPrompt
	default:
		return genericReplanPrompt
	}
}

const tvReplanPrompt = `Task: You are an AI system that revises a TV series file organization plan based on user feedback.

You must analyze the previous plan together with the user's hint and produce a corrected plan. Do not re-search metadata; reuse the previous plan as the baseline.

Specifics:
1. Input:
   - JSON object containing:
     - "root_path": the mandatory root prefix of every move target.
     - "files": the original array of file paths.
     - "previous_plan": the previous AI-generated plan actions.
     - "user_hint": the user's feedback or corrections.
2. Analyze:
   - Apply exactly the changes requested by the user hint (e.g. corrected series name, season/episode mapping, language folder).
   - Keep everything the hint does not mention as close to the previous plan as possible.
3. Construct new Jellyfin-compatible relative paths:
   - Folder: {root_path}/{Lang}/<Series Name (Year)>/Season XX
   - Video:  {root_path}/{Lang}/<Series Name (Year)>/Season XX/<Series Name (Year)> SXXEYY.ext
   - Every move target must start with the mandatory root prefix.
4. Edge cases:
   - Extras, samples or unmatchable files: "action": "skip" (omit target).
   - Every input file must appear exactly once in the plan.

Respond strictly following the required JSON schema.

Input:
%s`

const movieReplanPrompt = `Task: You are an AI system that revises a movie file organization plan based on user feedback.

You must analyze the previous plan together with the user's hint and produce a corrected plan. Do not re-search metadata; reuse the previous plan as the baseline.

Specifics:
1. Input:
   - JSON object containing:
     - "root_path": the mandatory root prefix of every move target.
     - "files": the original array of file paths.
     - "previous_plan": the previous AI-generated plan actions.
     - "user_hint": the user's feedback or corrections.
2. Analyze:
   - Apply exactly the changes requested by the user hint (e.g. corrected movie name, year, language folder, feature vs sample identification).
   - Keep everything the hint does not mention as close to the previous plan as possible.
3. Construct new Jellyfin-compatible relative paths:
   - Folder: {root_path}/{Lang}/<Movie Name (Year)>
   - Video:  {root_path}/{Lang}/<Movie Name (Year)>/<Movie Name (Year)>.ext
   - Every move target must start with the mandatory root prefix.
4. Edge cases:
   - Samples, trailers and extras: "action": "skip" (omit target).
   - Every input file must appear exactly once in the plan.

Respond strictly following the required JSON schema.

Input:
%s`

const bangoReplanPrompt = `Task: You are an AI system that revises a bango (JAV) file organization plan based on user feedback.

You must analyze the previous plan together with the user's hint and produce a corrected plan. Do not re-search metadata; reuse the previous plan as the baseline.

Specifics:
1. Input:
   - JSON object containing:
     - "root_path": the mandatory root prefix of every move target.
     - "files": the original array of file paths.
     - "previous_plan": the previous AI-generated plan actions.
     - "user_hint": the user's feedback or corrections.
2. Analyze:
   - Apply exactly the changes requested by the user hint (e.g. corrected bango, actress folder, multi-volume part mapping, -C Chinese subtitle naming).
   - Keep everything the hint does not mention as close to the previous plan as possible.
3. Construct new relative paths following the JAV library convention:
   - {root_path}/<Actress>/<BANGO>.part.N.ext for multi-volume files; <BANGO>-C.ext keeps the Chinese subtitle marker.
   - Every move target must start with the mandatory root prefix.
4. Edge cases:
   - Extras and unmatchable files: "action": "skip" (omit target).
   - Every input file must appear exactly once in the plan.

Respond strictly following the required JSON schema.

Input:
%s`

const genericReplanPrompt = `Task: You are an AI system that revises file organization plans based on user feedback.

You must analyze the original plan request, the previous AI-generated response, and the user's hint to create an improved plan.

Specifics:
1. Input:
   - "original_request": the original files and metadata.
   - "previous_response": the previous plan response.
   - "user_hint": feedback or corrections from the user.
2. Analysis:
   - Understand what the user wants to change based on their hint.
   - Identify issues with the previous plan.
   - Consider the original files and metadata.
3. Generate improved plan:
   - Fix issues identified in the user hint.
   - Maintain consistency with file organization best practices.
   - Ensure all file paths and actions are valid relative paths that never escape the media library.
4. Response format:
   - Return a plan containing one action per file.
   - Each action must be either "move" (with a target path) or "skip" (omit target).
5. Edge cases:
   - If the user hint is unclear, make reasonable assumptions.
   - Every input file must appear exactly once in the plan.

Respond strictly following the required JSON schema.

Input:
%s`
