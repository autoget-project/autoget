package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/autoget-project/organizer/internal/model"
	"github.com/autoget-project/organizer/internal/service"
)

// ExecuteHandler serves POST /v1/execute.
type ExecuteHandler struct {
	executor *service.Executor
}

// NewExecuteHandler creates a new ExecuteHandler.
func NewExecuteHandler(e *service.Executor) *ExecuteHandler {
	return &ExecuteHandler{executor: e}
}

// Handle physically executes the plan. Any aggregated failed_move entry
// results in HTTP 400 carrying the full failure list; a fully successful run
// (including source directory archiving) returns HTTP 200 with an empty list.
func (h *ExecuteHandler) Handle(w http.ResponseWriter, r *http.Request) {
	var req model.APIExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	resp, err := h.executor.ExecutePlan(r.Context(), req.Dir, req.Plan)
	if err != nil {
		// Fatal internal failure (e.g. archive rename error) -> 500.
		http.Error(w, fmt.Sprintf("execute failed: %v", err), http.StatusInternalServerError)
		return
	}

	if len(resp.FailedMove) > 0 {
		writeJSON(w, http.StatusBadRequest, resp)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
