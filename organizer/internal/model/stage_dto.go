package model

// ClassifierResult represents the outcome of Stage 1 classification.
type ClassifierResult struct {
	Category Category               `json:"category"`
	NeedLLM  bool                   `json:"need_llm"`
	Entities map[string]interface{} `json:"entities,omitempty"`
}

// EnrichedMetadata holds enriched details from Stage 2 metadata providers.
type EnrichedMetadata struct {
	Title         string   `json:"title"`
	OriginalTitle string   `json:"original_title,omitempty"`
	Year          int      `json:"year,omitempty"`
	Season        int      `json:"season,omitempty"`
	Episode       int      `json:"episode,omitempty"`
	IsAnim        bool     `json:"is_anim"`
	Bango         string   `json:"bango,omitempty"`
	Actors        []string `json:"actors,omitempty"`
	Maker         string   `json:"maker,omitempty"`
	IsVR          bool     `json:"is_vr"`
	FromMadou     bool     `json:"from_madou"`
	Language      Language `json:"language,omitempty"`
}

// DomainPlanResult holds the result of a Stage 3 domain planner.
type DomainPlanResult struct {
	Actions []PlanAction `json:"actions"`
}

// SubtitleMatchItem represents a subtitle candidate and its context for Stage 4.
type SubtitleMatchItem struct {
	Filename       string `json:"filename"`
	ContentPreview string `json:"content_preview"` // First 30 lines preview
}
