package stage1classifier

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/model"
)

// SearchContext carries the grounder's answers to a fixed set of questions
// about the real release (discovered via web search). Each field is the
// answer to one question and is consumed downstream: the checkers/arbiter use
// them to classify, and after the winner is chosen they are overlaid onto the
// Stage 1 entities so Stage 2/3 consume the same verified facts.
type SearchContext struct {
	SearchSummary string   `json:"search_summary,omitempty"` // supporting evidence/sources
	DetectedType  string   `json:"detected_type,omitempty"`  // media type answer
	OfficialTitle string   `json:"official_title,omitempty"` // official title answer
	Studio        string   `json:"studio,omitempty"`         // canonical studio/site answer
	Actors        []string `json:"actors,omitempty"`         // full performer names (abbreviations resolved)
	// IsVR is a verified fact from the search results: the release/studio is
	// Virtual Reality content. Only set when the search evidence says so.
	IsVR bool `json:"is_vr,omitempty"`
	// ReleaseDate is the confirmed release date in ISO YYYY-MM-DD, resolving
	// any ambiguous or re-ordered date groups in the filename. The year is
	// derived from it downstream; there is deliberately no separate year
	// field, so the model never has to guess an unguided numeric answer.
	ReleaseDate string `json:"release_date,omitempty"`
	// Series names a numbered/episodic series or JAV-style bango the release
	// belongs to, when the search confirms one.
	Series string `json:"series,omitempty"`
}

// HasInfo checks if any factual information was found.
func (s SearchContext) HasInfo() bool {
	return s.SearchSummary != "" || s.DetectedType != "" || s.OfficialTitle != "" || s.Studio != "" || len(s.Actors) > 0 || s.IsVR || s.ReleaseDate != "" || s.Series != ""
}

const searchGrounderPrompt = `You are a media intelligence research assistant. Identify the real release this download corresponds to and answer the questions below ONLY from the search results. The filename/metadata are clues for building the search query (strip resolution, codec and release-group noise) — they are NOT authoritative answers, and any ambiguous encoding in them must be resolved by what the search actually verifies.

Depth guidance — do not waste searches on answers the pipeline will get from authoritative catalogs:
- Some categories (movie, tv_series, music, and most mainstream titles) are matched against authoritative catalogs (e.g. TMDB) later in the pipeline, which supply the definitive studio and cast. For those, Q2/Q3 precision has no downstream value: give only what is plainly visible, or leave them empty — never run extra searches to resolve a studio alias or an abbreviated performer name.
- Other categories (Western adult / porn, JAV, independent or niche releases) have NO reliable catalog downstream: their studio (Q2), full performer names (Q3) and release date (Q5) are the anchors that later lookup steps rely on. For those, resolve filename abbreviations into the canonical full form the search confirms (e.g. "azul.h" -> "Azul Hermosa") and state the canonical studio name ("Tushy Raw", not the filename's "TushyRaw").
- Q1 (type), Q4 (official title), Q6 (is_vr) and Q7 (series) are cheap and always worth answering when the search shows them.

Answer each question with one concise value; leave a field empty when the search cannot verify an answer:
Q1 (detected_type): What type of content is this? One of: "Western adult video / porn", "Japanese adult video / JAV", "movie", "tv_series", "music", "audiobook", "photobook", or "unknown".
Q2 (studio): Which studio, site or network released it? Canonical name (e.g. "GirlsWay", "Tushy Raw", "SLR Originals"), not a filename alias — to the depth justified by the guidance above.
Q3 (actors): Who are the performers or actors? Full real names, abbreviations resolved — to the depth justified by the guidance above.
Q4 (official_title): What is the official title of the release / scene / episode?
Q5 (release_date): What is the exact release date, as YYYY-MM-DD? If the filename's date groups are ambiguous (e.g. "26.07.19") or wrongly ordered, use the search results to state the real release date.
Q6 (is_vr): Is this Virtual Reality (VR) content? Set true ONLY when the search evidence says so (a VR studio/release, e.g. SLR Originals, VR Bangers, CzechVR).
Q7 (series): Does it belong to a numbered/episodic series, or carry a JAV-style bango or series code? Name it if the search confirms one.
Finally, search_summary: 1-2 sentences of factual summary with the source titles/URLs that back these answers.

Return your answer strictly matching the required JSON schema.`

// GroundWithSearch runs a single search query across the files if the provider supports SearchProvider.
// If provider does not support search or search fails, it returns an empty SearchContext gracefully without failing.
// A non-nil replan is attached as a dedicated "replan" field, separate from
// the upstream metadata, so the grounder knows the previous plan is suspect.
func GroundWithSearch(ctx context.Context, provider ai.Provider, files []string, metadata map[string]interface{}, replan *model.ReplanContext) SearchContext {
	sp, ok := provider.(ai.SearchProvider)
	if !ok {
		return SearchContext{}
	}

	payload := map[string]interface{}{
		"files":    files,
		"metadata": metadata,
	}
	if replan != nil {
		payload["replan"] = replan
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return SearchContext{}
	}

	prompt := fmt.Sprintf("%s\n\nInput:\n%s", searchGrounderPrompt, string(payloadBytes))
	var result SearchContext
	if err := sp.GenerateStructuredWithSearch(ctx, prompt, SearchContext{}, &result); err != nil {
		// Non-fatal: degrade gracefully if search fails
		return SearchContext{}
	}
	return result
}
