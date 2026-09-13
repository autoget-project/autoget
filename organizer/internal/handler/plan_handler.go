// Package handler implements the REST route layer (/v1/plan, /v1/execute,
// /v1/replan-with-hint) of the organizer service.
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/autoget-project/organizer/internal/model"
	"github.com/autoget-project/organizer/internal/pipeline"
)

// PlanHandler serves POST /v1/plan.
type PlanHandler struct {
	pipeline *pipeline.Pipeline
}

// NewPlanHandler creates a new PlanHandler.
func NewPlanHandler(p *pipeline.Pipeline) *PlanHandler {
	return &PlanHandler{pipeline: p}
}

// Handle processes a plan creation request through the full 4-stage pipeline.
// Fatal unrecoverable internal errors map to HTTP 500; normal planning (including
// the unknown category) keeps the response contract with error set to null.
func (h *PlanHandler) Handle(w http.ResponseWriter, r *http.Request) {
	var req model.APIPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	resp, err := h.pipeline.CreatePlan(r.Context(), req.Dir, req.Files, req.Metadata)
	if err != nil {
		// Fatal internal failure -> 500 while preserving the response shape.
		msg := err.Error()
		writeJSON(w, http.StatusInternalServerError, model.PlanResponse{Plan: []model.PlanAction{}, Error: &msg})
		return
	}

	// Normal planning: error stays null (contract compatibility).
	writeJSON(w, http.StatusOK, resp)
}

// writeJSON serializes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers are already sent; nothing more can be recovered here.
		return
	}
}
