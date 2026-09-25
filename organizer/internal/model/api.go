package model

import "github.com/autoget-project/autoget/protocol"

// Constants mirrored from protocol module for convenient local access.
const (
	ActionMove = protocol.ActionMove
	ActionSkip = protocol.ActionSkip
)

// PlanAction represents a file movement or skipping action.
// Type alias to protocol.PlanAction.
type PlanAction = protocol.PlanAction

// PlanRequest represents the input parameters for creating a plan.
// Type alias to protocol.PlanRequest.
type PlanRequest = protocol.PlanRequest

// APIPlanRequest represents the REST API request for creating a plan.
// Type alias to protocol.APIPlanRequest.
type APIPlanRequest = protocol.APIPlanRequest

// MoverResponse is the internal mover output DTO.
// Type alias to protocol.MoverResponse.
type MoverResponse = protocol.MoverResponse

// PlanResponse represents the REST API response for creating a plan.
// Type alias to protocol.PlanResponse.
type PlanResponse = protocol.PlanResponse

// ExecuteRequest represents internal execution parameters.
// Type alias to protocol.ExecuteRequest.
type ExecuteRequest = protocol.ExecuteRequest

// APIExecuteRequest represents the REST API request for executing a plan.
// Type alias to protocol.APIExecuteRequest.
type APIExecuteRequest = protocol.APIExecuteRequest

// PlanFailed details a failed plan action during execution.
// Type alias to protocol.PlanFailed.
type PlanFailed = protocol.PlanFailed

// ExecuteResponse represents the REST API response for plan execution.
// Type alias to protocol.ExecuteResponse.
type ExecuteResponse = protocol.ExecuteResponse

// APIReplanRequest represents the REST API request for the unified replan
// endpoint. Type alias to protocol.APIReplanRequest.
type APIReplanRequest = protocol.APIReplanRequest

// APIReplanWithHintRequest represents the legacy replan-with-hint request.
// Type alias to protocol.APIReplanWithHintRequest.
type APIReplanWithHintRequest = protocol.APIReplanWithHintRequest
