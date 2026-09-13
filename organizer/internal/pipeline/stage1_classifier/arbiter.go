package stage1classifier

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/model"
)

// ArbiterDecision is the structured output from the Arbiter LLM.
type ArbiterDecision struct {
	Category model.Category  `json:"category"`
	Reason   string          `json:"reason"`
	Entities CheckerEntities `json:"entities"`
}

const arbiterSystemPrompt = `You are an expert media categorization arbiter.
Multiple specialized classifiers have analyzed a media item and provided their findings (confidence, reasons, and extracted entities).
Your job is to make the final authoritative categorization decision.

Instructions:
1. Review the input files, metadata, and all specialist findings.
2. Resolve any conflicts or ambiguities among the specialists:
   - For adult content: if there is no valid Japanese/Asian bango code (like SSIS-123, FC2-PPV, MD-0123), it belongs to "porn" (Western/general adult), NOT "bango_porn".
   - If one specialist provides much stronger, grounded reasoning, favor that decision.
   - If multiple specialists reported "maybe", weigh the file naming patterns and metadata.
3. Extract clean title, year, actors, bango, imdb_id, or is_vr where applicable. is_vr is a semantic judgment: set it true when the content is Virtual Reality (VR) adult video (VR studio/series/release, per the search context or naming), never from a filename token rule alone.
4. NEVER return "unknown" for clearly recognizable media: if the file names, metadata, specialist reasons, or your own analysis identify the content type, you MUST choose that category. Reserve "unknown" strictly for non-media or truly unrecognizable content (e.g. software installers, games, random data).

Return your answer strictly matching the required JSON schema.`

type ArbiterInputSpecialist struct {
	Category   model.Category  `json:"category"`
	Confidence Confidence      `json:"confidence"`
	Reason     string          `json:"reason"`
	Entities   CheckerEntities `json:"entities"`
}

type ArbiterInputPayload struct {
	Files         []string                 `json:"files"`
	Metadata      map[string]interface{}   `json:"metadata"`
	SearchContext *SearchContext           `json:"search_context,omitempty"`
	Specialists   []ArbiterInputSpecialist `json:"specialists"`
}

// DecideArbiter invokes the arbiter LLM to resolve multiple or conflicting checker findings.
func DecideArbiter(ctx context.Context, provider ai.Provider, files []string, metadata map[string]interface{}, results []CheckerResult, searchCtx SearchContext) (ArbiterDecision, error) {
	if provider == nil {
		return ArbiterDecision{Category: model.CategoryUnknown}, fmt.Errorf("ai provider is nil")
	}

	specs := make([]ArbiterInputSpecialist, 0, len(results))
	for _, res := range results {
		if res.Err == nil {
			specs = append(specs, ArbiterInputSpecialist{
				Category:   res.Category,
				Confidence: res.Response.Confidence,
				Reason:     res.Response.Reason,
				Entities:   res.Response.Entities,
			})
		}
	}

	payload := ArbiterInputPayload{
		Files:       files,
		Metadata:    metadata,
		Specialists: specs,
	}
	if searchCtx.HasInfo() {
		payload.SearchContext = &searchCtx
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return ArbiterDecision{Category: model.CategoryUnknown}, fmt.Errorf("failed to marshal arbiter payload: %w", err)
	}

	prompt := fmt.Sprintf("%s\n\nInput:\n%s", arbiterSystemPrompt, string(payloadBytes))

	var decision ArbiterDecision
	if err := provider.GenerateStructured(ctx, prompt, ArbiterDecision{}, &decision); err != nil {
		return ArbiterDecision{Category: model.CategoryUnknown}, fmt.Errorf("arbiter llm failed: %w", err)
	}

	if !isValidCategory(decision.Category) {
		decision.Category = model.CategoryUnknown
	}

	return decision, nil
}
