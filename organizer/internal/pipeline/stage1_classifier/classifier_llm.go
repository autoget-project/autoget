package stage1classifier

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/model"
)

// ClassifierLLMResponse defines the structured JSON output schema (kept for backward compatibility and test mock mapping).
type ClassifierLLMResponse struct {
	Category model.Category  `json:"category"`
	Reason   string          `json:"reason"`
	Entities CheckerEntities `json:"entities"`
}

// ClassifierLLM handles LLM classification using concurrent specialist checkers and an arbiter.
type ClassifierLLM struct {
	provider ai.Provider
}

// ClassifierDetail captures intermediate results from LLM classification for diagnostics.
type ClassifierDetail struct {
	SearchContext SearchContext
	Specialists   []CheckerResult
	ArbiterUsed   bool
	ArbiterReason string
}

// NewClassifierLLM creates a new ClassifierLLM instance.
func NewClassifierLLM(provider ai.Provider) *ClassifierLLM {
	return &ClassifierLLM{provider: provider}
}

// Classify runs specialist checkers concurrently and arbitrates results.
func (c *ClassifierLLM) Classify(ctx context.Context, files []string, metadata map[string]interface{}) (model.ClassifierResult, error) {
	res, _, err := c.ClassifyWithDetail(ctx, files, metadata)
	return res, err
}

// ClassifyWithDetail runs specialist checkers and returns diagnostic details along with the result.
func (c *ClassifierLLM) ClassifyWithDetail(ctx context.Context, files []string, metadata map[string]interface{}) (model.ClassifierResult, ClassifierDetail, error) {
	if c.provider == nil {
		return model.ClassifierResult{
			Category: model.CategoryUnknown,
			NeedLLM:  true,
		}, ClassifierDetail{}, fmt.Errorf("classifier llm provider is nil")
	}

	// Backward compatibility check for mock provider / legacy single-pass prompt:
	if c.provider.Name() == "mock" {
		legacyPrompt := fmt.Sprintf("You are an expert media categorization assistant.\n\nInput:\nfiles: %v\nmetadata: %v", files, metadata)
		var legacyResp ClassifierLLMResponse
		err := c.provider.GenerateStructured(ctx, legacyPrompt, ClassifierLLMResponse{}, &legacyResp)
		if err == nil && legacyResp.Category != "" {
			return model.ClassifierResult{
				Category: legacyResp.Category,
				NeedLLM:  true,
				Entities: entitiesToMap(legacyResp.Entities, legacyResp.Reason),
			}, ClassifierDetail{}, nil
		} else if err != nil && !strings.Contains(err.Error(), "no matching rule") {
			return model.ClassifierResult{Category: model.CategoryUnknown, NeedLLM: true}, ClassifierDetail{}, err
		}
	}

	// Step 0: Single search grounding pass (only if provider supports search, e.g. Gemini)
	searchCtx := GroundWithSearch(ctx, c.provider, files, metadata)
	detail := ClassifierDetail{
		SearchContext: searchCtx,
	}

	candidates := selectCandidates(files, metadata)
	results := make([]CheckerResult, len(candidates))

	var wg sync.WaitGroup

	for i, cat := range candidates {
		promptTpl, ok := getCheckerPrompt(cat)
		if !ok {
			results[i] = CheckerResult{Category: cat, Response: CheckerResponse{Confidence: ConfidenceNo}}
			continue
		}

		wg.Add(1)
		go func(idx int, category model.Category, tpl string) {
			defer wg.Done()
			resp, err := runSpecialistChecker(ctx, c.provider, category, tpl, files, metadata, searchCtx)
			results[idx] = CheckerResult{
				Category: category,
				Response: resp,
				Err:      err,
			}
		}(i, cat, promptTpl)
	}

	wg.Wait()
	detail.Specialists = results

	// Check if all checkers failed with error
	var firstErr error
	errCount := 0
	for _, res := range results {
		if res.Err != nil {
			errCount++
			if firstErr == nil {
				firstErr = res.Err
			}
		}
	}
	if len(results) > 0 && errCount == len(results) {
		return model.ClassifierResult{Category: model.CategoryUnknown, NeedLLM: true}, detail, fmt.Errorf("all specialist checkers failed: %w", firstErr)
	}

	// Analyze checker outputs
	var yesResults []CheckerResult
	var maybeResults []CheckerResult

	for _, res := range results {
		if res.Err != nil {
			continue
		}
		switch res.Response.Confidence {
		case ConfidenceYes:
			yesResults = append(yesResults, res)
		case ConfidenceMaybe:
			maybeResults = append(maybeResults, res)
		}
	}

	// Fast path: Exactly one specialist returned "yes" with no conflicts
	if len(yesResults) == 1 {
		chosen := yesResults[0]
		return model.ClassifierResult{
			Category: chosen.Category,
			NeedLLM:  true,
			Entities: entitiesFor(chosen.Response.Entities, chosen.Response.Reason, searchCtx),
		}, detail, nil
	}

	// If no yes, but exactly one maybe and no other maybes or yeses
	if len(yesResults) == 0 && len(maybeResults) == 1 {
		chosen := maybeResults[0]
		return model.ClassifierResult{
			Category: chosen.Category,
			NeedLLM:  true,
			Entities: entitiesFor(chosen.Response.Entities, chosen.Response.Reason, searchCtx),
		}, detail, nil
	}

	// If all checkers explicitly returned ConfidenceNo, we can safely treat as unknown without forcing arbiter error
	allNo := len(yesResults) == 0 && len(maybeResults) == 0

	// Ambiguous, multiple "yes" conflicts, multiple "maybe", or all "no": call Arbiter
	detail.ArbiterUsed = true
	decision, err := DecideArbiter(ctx, c.provider, files, metadata, results, searchCtx)
	if err != nil {
		// Fallback: if we had at least one yes, take the first one
		if len(yesResults) > 0 {
			return model.ClassifierResult{
				Category: yesResults[0].Category,
				NeedLLM:  true,
				Entities: entitiesFor(yesResults[0].Response.Entities, yesResults[0].Response.Reason, searchCtx),
			}, detail, nil
		}
		// If all checkers returned "no", it is legitimately unknown
		if allNo {
			return model.ClassifierResult{Category: model.CategoryUnknown, NeedLLM: true}, detail, nil
		}
		return model.ClassifierResult{Category: model.CategoryUnknown, NeedLLM: true}, detail, err
	}

	detail.ArbiterReason = decision.Reason

	return model.ClassifierResult{
		Category: decision.Category,
		NeedLLM:  true,
		Entities: entitiesFor(decision.Entities, decision.Reason, searchCtx),
	}, detail, nil
}

// entitiesFor builds the Stage 1 entities from a checker/arbiter extraction
// and overlays the web-search-verified answers (entitiesToMap + mergeSearchFacts).
func entitiesFor(e CheckerEntities, reason string, searchCtx SearchContext) map[string]interface{} {
	m := entitiesToMap(e, reason)
	mergeSearchFacts(m, searchCtx)
	return m
}

func entitiesToMap(e CheckerEntities, reason string) map[string]interface{} {
	m := make(map[string]interface{})
	if e.IMDbID != "" {
		m["imdb_id"] = e.IMDbID
	}
	if e.DmmID != "" {
		m["dmm_id"] = e.DmmID
	}
	if e.Bango != "" {
		m["bango"] = e.Bango
	}
	if e.CleanTitle != "" {
		m["clean_title"] = e.CleanTitle
	}
	if e.Year > 0 {
		m["year"] = e.Year
	}
	if len(e.Actors) > 0 {
		m["actors"] = e.Actors
	}
	if e.IsVR {
		m["is_vr"] = true
	}
	if reason != "" {
		m["reason"] = reason
	}
	return m
}

// mergeSearchFacts overlays the web-search-verified answers (the grounder's
// question set) onto the Stage 1 entities so downstream stages consume the
// same grounded facts. Precedence rules:
//   - Identity answers verified by search win: official title, full performer
//     names, canonical studio, confirmed release date, series. The release
//     year is derived from the confirmed release date.
//   - IsVR is only ever set true by grounding (a verified fact); a grounding
//     "false" never clears a checker's true, because search can miss.
//
// Keys: clean_title / actors / year / is_vr (existing), plus studio /
// release_date / series / detected_type / search_summary (informational).
func mergeSearchFacts(m map[string]interface{}, s SearchContext) {
	if m == nil {
		return
	}
	if s.OfficialTitle != "" {
		m["clean_title"] = s.OfficialTitle
	}
	if len(s.Actors) > 0 {
		m["actors"] = s.Actors
	}
	if s.Studio != "" {
		m["studio"] = s.Studio
	}
	if s.ReleaseDate != "" {
		m["release_date"] = s.ReleaseDate
		if y := releaseDateYear(s.ReleaseDate); y > 0 {
			m["year"] = y
		}
	}
	if s.Series != "" {
		m["series"] = s.Series
	}
	if s.DetectedType != "" {
		m["detected_type"] = s.DetectedType
	}
	if s.IsVR {
		m["is_vr"] = true
	}
	if s.SearchSummary != "" {
		m["search_summary"] = s.SearchSummary
	}
}

// releaseDateYear extracts the 4-digit year from an ISO YYYY-MM-DD date.
func releaseDateYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return y
}

// ClassifyPipeline executes Rule matcher first, falling back to ClassifierLLM if unmatched.
func ClassifyPipeline(ctx context.Context, provider ai.Provider, files []string, metadata map[string]interface{}) (model.ClassifierResult, error) {
	res, _, err := ClassifyPipelineWithDetail(ctx, provider, files, metadata)
	return res, err
}

// ClassifyPipelineWithDetail executes Rule matcher first, falling back to ClassifierLLM with detailed diagnostics.
func ClassifyPipelineWithDetail(ctx context.Context, provider ai.Provider, files []string, metadata map[string]interface{}) (model.ClassifierResult, ClassifierDetail, error) {
	res, matched := MatchByRules(files, metadata)
	if matched {
		return res, ClassifierDetail{}, nil
	}

	llm := NewClassifierLLM(provider)
	return llm.ClassifyWithDetail(ctx, files, metadata)
}
