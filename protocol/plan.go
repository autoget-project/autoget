package protocol

const (
	// ActionMove indicates that the file should be moved.
	ActionMove = "move"
	// ActionSkip indicates that the file should be skipped.
	ActionSkip = "skip"
)

// PlanAction represents a file movement or skipping action.
// In skip action, Target should be serialized as null (Target *string without omitempty)
// to maintain exact contract alignment with upstream clients.
type PlanAction struct {
	File   string  `json:"file"`
	Action string  `json:"action"` // "move" or "skip"
	Target *string `json:"target"` // pointer ensures "target": null when nil
}

// PlanRequest represents the input parameters for creating a plan.
type PlanRequest struct {
	Files    []string               `json:"files"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// APIPlanRequest represents the REST API request for creating a plan.
type APIPlanRequest struct {
	Dir      string                 `json:"dir"`
	Files    []string               `json:"files"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// MoverResponse is the internal mover output DTO. Planners that already
// return []PlanAction use it implicitly; it exists for wire/contract parity.
type MoverResponse struct {
	Plan []PlanAction `json:"plan"`
}

// PlanResponse represents the REST API response for creating a plan.
type PlanResponse struct {
	Plan  []PlanAction `json:"plan"`
	Error *string      `json:"error"` // pointer ensures "error": null when nil
}

// ExecuteRequest represents internal execution parameters.
type ExecuteRequest struct {
	Plan []PlanAction `json:"plan"`
}

// APIExecuteRequest represents the REST API request for executing a plan.
type APIExecuteRequest struct {
	Dir  string       `json:"dir"`
	Plan []PlanAction `json:"plan"`
}

// PlanFailed details a failed plan action during execution.
type PlanFailed struct {
	File   string  `json:"file"`
	Action string  `json:"action"`
	Target *string `json:"target"`
	Reason string  `json:"reason"`
}

// ExecuteResponse represents the REST API response for plan execution.
type ExecuteResponse struct {
	FailedMove []PlanFailed `json:"failed_move"`
}

// APIReplanRequest represents the unified replan request. The previous result
// is a dedicated field, deliberately NOT merged into Metadata: it is known to
// be flawed (that is why the user is replanning) and must never be mistaken for
// authoritative upstream metadata. Dir is the download sub-directory, used by
// Stage 4 to pair companion subtitles.
type APIReplanRequest struct {
	Dir            string                 `json:"dir"`
	Files          []string               `json:"files"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	PreviousResult *PlanResponse          `json:"previous_result"`
	UserHint       string                 `json:"user_hint,omitempty"`
}

// APIReplanWithHintRequest represents the legacy /v1/replan-with-hint request
// shape (retained for wire compatibility).
type APIReplanWithHintRequest struct {
	Dir              string                 `json:"dir"`
	Files            []string               `json:"files"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	PreviousResponse *PlanResponse          `json:"previous_response"`
	UserHint         string                 `json:"user_hint"`
}

// StringPtr returns a pointer to the passed string value.
func StringPtr(s string) *string {
	return &s
}
